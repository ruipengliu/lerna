//go:build darwin

#include "internal/barrier/sqlite3.h"
#include <fcntl.h>
#include <stdio.h>
#include <stdarg.h>
#include <unistd.h>
#include <string.h>

typedef struct BarrierFile { sqlite3_file file; sqlite3_file *real; char *directory; int dirsync; } BarrierFile;
static sqlite3_vfs *parent;
static sqlite3_vfs wrapper;
static int (*real_fcntl)(int,int,...);
static int (*directory_fsync)(int)=fsync;
/* 与 Unix VFS 的系统调用替换接口一致；生产没有故障开关。 */
static sqlite3_syscall_ptr get_call(sqlite3_vfs *v,const char *name){
 if(strcmp(name,"fsync")==0)return (sqlite3_syscall_ptr)directory_fsync;
 return parent->xGetSystemCall(parent,name);
}
static int set_call(sqlite3_vfs *v,const char *name,sqlite3_syscall_ptr call){
 if(strcmp(name,"fsync")==0){directory_fsync=call?(int(*)(int))call:fsync;return SQLITE_OK;}
 return parent->xSetSystemCall(parent,name,call);
}
/* SQLite 自身忽略部分目录同步错误；这里必须检查返回值。 */
static int sync_directory(const char *directory){
 int fd=open(directory,O_RDONLY|O_DIRECTORY);if(fd<0)return SQLITE_IOERR_DIR_FSYNC;
 int rc=directory_fsync(fd);
 if(rc==0){int(*call)(int,int,...)=(int(*)(int,int,...))parent->xGetSystemCall(parent,"fcntl");rc=call(fd,F_FULLFSYNC,0);}
 int closed=close(fd);
 return rc==0 && closed==0?SQLITE_OK:SQLITE_IOERR_DIR_FSYNC;
}
static _Thread_local int full_seen, full_failed;
/* 固定 SQLite 构建的 fcntl 整数参数与锁指针参数分开转发。 */
static int strict_fcntl(int fd,int op,...) {
 va_list ap;va_start(ap,op);int rc;
 if(op==F_GETFD || op==F_SETFD || op==F_FULLFSYNC) {
  int arg=va_arg(ap,int);rc=real_fcntl(fd,op,arg);
  if(op==F_FULLFSYNC){full_seen=1;if(rc!=0)full_failed=1;}
 } else {void *arg=va_arg(ap,void*);rc=real_fcntl(fd,op,arg);}
 va_end(ap);return rc;
}
#define F(p) ((BarrierFile *)(p))
#define R(p) (F(p)->real)
#define M(p) (R(p)->pMethods)
static int close_file(sqlite3_file *p) { int rc=M(p)->xClose(R(p)); sqlite3_free(R(p)); sqlite3_free(F(p)->directory); p->pMethods=0; return rc; }
static int read_file(sqlite3_file *p,void *b,int n,sqlite3_int64 o){return M(p)->xRead(R(p),b,n,o);}
static int write_file(sqlite3_file *p,const void *b,int n,sqlite3_int64 o){return M(p)->xWrite(R(p),b,n,o);}
static int truncate_file(sqlite3_file *p,sqlite3_int64 n){return M(p)->xTruncate(R(p),n);}
static int sync_file(sqlite3_file *p,int flags){
 if(F(p)->dirsync){int rc=sync_directory(F(p)->directory);if(rc!=SQLITE_OK)return rc;F(p)->dirsync=0;}
 full_seen=0;full_failed=0;
 int rc=M(p)->xSync(R(p),flags);
 /* 不把 Unix VFS 的 fsync 回退误报为完整设备屏障。 */
 if((flags&0x0f)==SQLITE_SYNC_FULL && (!full_seen || full_failed))return SQLITE_IOERR_FSYNC;
 return rc;}
static int size_file(sqlite3_file *p,sqlite3_int64 *n){return M(p)->xFileSize(R(p),n);}
static int lock_file(sqlite3_file *p,int f){return M(p)->xLock(R(p),f);}
static int unlock_file(sqlite3_file *p,int f){return M(p)->xUnlock(R(p),f);}
static int check_file(sqlite3_file *p,int *r){return M(p)->xCheckReservedLock(R(p),r);}
static int control_file(sqlite3_file *p,int op,void *arg){return M(p)->xFileControl(R(p),op,arg);}
static int sector_file(sqlite3_file *p){return M(p)->xSectorSize(R(p));}
static int device_file(sqlite3_file *p){return M(p)->xDeviceCharacteristics(R(p));}
static int shm_map(sqlite3_file *p,int i,int n,int w,void volatile **r){return M(p)->xShmMap(R(p),i,n,w,r);}
static int shm_lock(sqlite3_file *p,int o,int n,int f){return M(p)->xShmLock(R(p),o,n,f);}
static void shm_barrier(sqlite3_file *p){M(p)->xShmBarrier(R(p));}
static int shm_unmap(sqlite3_file *p,int d){return M(p)->xShmUnmap(R(p),d);}
static int fetch_file(sqlite3_file *p,sqlite3_int64 o,int n,void **r){*r=0;return SQLITE_OK;}
static int unfetch_file(sqlite3_file *p,sqlite3_int64 o,void *r){return SQLITE_OK;}
static sqlite3_io_methods methods={3,close_file,read_file,write_file,truncate_file,sync_file,size_file,lock_file,unlock_file,check_file,control_file,sector_file,device_file,shm_map,shm_lock,shm_barrier,shm_unmap,fetch_file,unfetch_file};
static sqlite3_io_methods basic_methods={1,close_file,read_file,write_file,truncate_file,sync_file,size_file,lock_file,unlock_file,check_file,control_file,sector_file,device_file};
static int open_file(sqlite3_vfs *v,const char *name,sqlite3_file *file,int flags,int *out){
 BarrierFile *p=F(file); memset(p,0,sizeof(*p)); p->real=sqlite3_malloc(parent->szOsFile); if(!p->real)return SQLITE_NOMEM;
 memset(p->real,0,parent->szOsFile); int rc=parent->xOpen(parent,name,p->real,flags,out);
 if(rc!=SQLITE_OK){if(p->real->pMethods)p->real->pMethods->xClose(p->real);sqlite3_free(p->real);p->real=0;return rc;}
 if((flags&SQLITE_OPEN_MAIN_DB) && (p->real->pMethods->iVersion<2 || !p->real->pMethods->xShmMap)){p->real->pMethods->xClose(p->real);sqlite3_free(p->real);return SQLITE_CANTOPEN;}
 if(name && (flags&SQLITE_OPEN_CREATE)){
  const char *slash=strrchr(name,'/');
  if(!slash){p->real->pMethods->xClose(p->real);sqlite3_free(p->real);return SQLITE_CANTOPEN;}
  p->directory=sqlite3_mprintf("%.*s",slash==name?1:(int)(slash-name),name);
  if(!p->directory){p->real->pMethods->xClose(p->real);sqlite3_free(p->real);return SQLITE_NOMEM;}
  p->dirsync=1;
 }
 p->file.pMethods=(flags&SQLITE_OPEN_MAIN_DB)?&methods:&basic_methods; return SQLITE_OK;
}
static int delete_file(sqlite3_vfs *v,const char *name,int syncdir){
 int rc=parent->xDelete(parent,name,0);if(rc!=SQLITE_OK || !syncdir)return rc;
 const char *slash=strrchr(name,'/');if(!slash)return SQLITE_IOERR_DIR_FSYNC;
 char *directory=sqlite3_mprintf("%.*s",slash==name?1:(int)(slash-name),name);if(!directory)return SQLITE_NOMEM;
 rc=sync_directory(directory);sqlite3_free(directory);return rc;
}
int register_strict_barrier(void){
 if(strcmp(sqlite3_sourceid(),SQLITE_SOURCE_ID)!=0)return SQLITE_MISMATCH;
 parent=sqlite3_vfs_find(0);if(!parent || parent->iVersion<3 || !parent->xGetSystemCall || !parent->xSetSystemCall)return SQLITE_NOTFOUND;
 real_fcntl=(int(*)(int,int,...))parent->xGetSystemCall(parent,"fcntl");
 if(!real_fcntl)return SQLITE_NOTFOUND;
 int rc=parent->xSetSystemCall(parent,"fcntl",(sqlite3_syscall_ptr)strict_fcntl);if(rc!=SQLITE_OK)return rc;
 wrapper=*parent;wrapper.zName="lerna-strict-fullfsync";wrapper.szOsFile=sizeof(BarrierFile);wrapper.xOpen=open_file;wrapper.xDelete=delete_file;wrapper.xGetSystemCall=get_call;wrapper.xSetSystemCall=set_call;wrapper.pNext=0;
 return sqlite3_vfs_register(&wrapper,1);
}

#include <sys/mount.h>
#include <sys/utsname.h>
#include <sys/sysctl.h>
/* 准入的是固定内核与 APFS 的等价模型条件，不是物理拔电认证。 */
void barrier_platform(const char *path,char *result,int length) {
 struct statfs fs;struct utsname os;char product[64],build[64];size_t pn=sizeof(product),bn=sizeof(build);
 if(statfs(path,&fs)!=0 || uname(&os)!=0 || sysctlbyname("kern.osproductversion",product,&pn,0,0)!=0 || sysctlbyname("kern.osversion",build,&bn,0,0)!=0){result[0]=0;return;}
 snprintf(result,length,"%s/%s/%s/%s/%s",product,build,os.release,os.machine,fs.f_fstypename);
}
