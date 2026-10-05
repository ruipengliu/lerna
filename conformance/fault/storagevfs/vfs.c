//go:build fault

#include "../../../infra/sqlite/internal/barrier/sqlite3.h"
#include <stdio.h>
#include <stdlib.h>
#include <stdint.h>
#include <stdarg.h>
#include <stdatomic.h>
#include <fcntl.h>
#include <errno.h>
#include <pthread.h>
#include <unistd.h>
#include <string.h>

typedef struct TraceFile { sqlite3_file file; sqlite3_file *real; int id, dirsync; } TraceFile;
static sqlite3_vfs *parent;
static sqlite3_vfs wrapper;
static FILE *trace;
static uint64_t sequence;
static char *database_path;
static _Thread_local int ephemeral_io;
static _Atomic int ephemeral_write_error;
static _Atomic uint64_t ephemeral_opens, ephemeral_reads, ephemeral_writes, ephemeral_closes, ephemeral_failures;
static _Atomic int mode, skip_sync;
static _Atomic uint64_t native_attempts, native_successes, native_failures, directory_failures;
#ifdef __APPLE__
static int (*real_fcntl)(int,int,...);
static int (*real_fsync)(int);
static int observed_fsync(int fd){
 if(!ephemeral_io && atomic_load(&mode)==4){atomic_fetch_add(&directory_failures,1);errno=EIO;return -1;}
 return real_fsync(fd);
}

/* 固定 SQLite 源码只传整数 GETFD/SETFD/FULLFSYNC，其余为锁指针。 */
static int observed_fcntl(int fd,int op,...) {
 va_list ap;va_start(ap,op);int rc;
 if(op==F_GETFD || op==F_SETFD || op==F_FULLFSYNC) {
  int arg=va_arg(ap,int);
  if(op==F_FULLFSYNC && !ephemeral_io) {
   atomic_fetch_add(&native_attempts,1);
   if(atomic_load(&mode)==3 && atomic_fetch_sub(&skip_sync,1)<=0) {errno=ENOTSUP;rc=-1;} else rc=real_fcntl(fd,op,arg);
   if(rc==0)atomic_fetch_add(&native_successes,1);
   else {atomic_fetch_add(&native_failures,1);}
  } else rc=real_fcntl(fd,op,arg);
 } else {void *arg=va_arg(ap,void*);rc=real_fcntl(fd,op,arg);}
 va_end(ap);return rc;
}
#endif
static pthread_mutex_t mutex = PTHREAD_MUTEX_INITIALIZER;
/* 跟踪文件在故障模型之外；丢失跟踪本身直接使测试失败。 */
static void record(int kind, int id, sqlite3_int64 offset, const void *data, int length) {
 unsigned char header[24]={0}; header[0]=kind; header[1]=id;
 uint64_t off=(uint64_t)offset;
 for(int i=0;i<8;i++) header[8+i]=(off>>(i*8))&255;
 for(int i=0;i<4;i++) header[16+i]=((uint32_t)length>>(i*8))&255;
 if(fwrite(header,1,24,trace)!=24 || (length && fwrite(data,1,length,trace)!=(size_t)length) || fflush(trace)) _exit(92);
 sequence++;
}

#define F(p) ((TraceFile *)(p))
#define R(p) (F(p)->real)
#define M(p) (R(p)->pMethods)
static int close_file(sqlite3_file *p) { int rc=M(p)->xClose(R(p)); sqlite3_free(R(p)); p->pMethods=0; return rc; }
static int read_file(sqlite3_file *p,void *b,int n,sqlite3_int64 o){return M(p)->xRead(R(p),b,n,o);}
static int write_file(sqlite3_file *p,const void *b,int n,sqlite3_int64 o){
 pthread_mutex_lock(&mutex); int rc=M(p)->xWrite(R(p),b,n,o);
 if(rc==SQLITE_OK)record('W',F(p)->id,o,b,n);
 pthread_mutex_unlock(&mutex); return rc;
}
static int truncate_file(sqlite3_file *p,sqlite3_int64 n){
 pthread_mutex_lock(&mutex); int rc=M(p)->xTruncate(R(p),n);
 if(rc==SQLITE_OK)record('T',F(p)->id,n,0,0);
 pthread_mutex_unlock(&mutex);return rc;
}
static int sync_file(sqlite3_file *p,int flags){
 pthread_mutex_lock(&mutex);
 int rc=(mode==2 && atomic_fetch_sub(&skip_sync,1)<=0)?SQLITE_IOERR_FSYNC:mode==1?SQLITE_OK:M(p)->xSync(R(p),flags);
 if(rc==SQLITE_OK && mode!=1){record('S',F(p)->id,flags,0,0);if(F(p)->dirsync){record('N',F(p)->id,0,0,0);F(p)->dirsync=0;}}
 pthread_mutex_unlock(&mutex);return rc;
}
static int size_file(sqlite3_file *p,sqlite3_int64 *n){return M(p)->xFileSize(R(p),n);}
static int lock_file(sqlite3_file *p,int f){return M(p)->xLock(R(p),f);}
static int unlock_file(sqlite3_file *p,int f){return M(p)->xUnlock(R(p),f);}
static int check_file(sqlite3_file *p,int *r){return M(p)->xCheckReservedLock(R(p),r);}
static int control_file(sqlite3_file *p,int op,void *arg){return M(p)->xFileControl(R(p),op,arg);}
static int sector_file(sqlite3_file *p){return 4096;}
static int device_file(sqlite3_file *p){return SQLITE_IOCAP_POWERSAFE_OVERWRITE;}
static int shm_map(sqlite3_file *p,int i,int n,int w,void volatile **r){return M(p)->xShmMap(R(p),i,n,w,r);}
static int shm_lock(sqlite3_file *p,int o,int n,int f){return M(p)->xShmLock(R(p),o,n,f);}
static void shm_barrier(sqlite3_file *p){M(p)->xShmBarrier(R(p));}
static int shm_unmap(sqlite3_file *p,int d){return M(p)->xShmUnmap(R(p),d);}
static int fetch_file(sqlite3_file *p,sqlite3_int64 o,int n,void **r){*r=0;return SQLITE_OK;}
static int unfetch_file(sqlite3_file *p,sqlite3_int64 o,void *r){return SQLITE_OK;}
static sqlite3_io_methods methods={3,close_file,read_file,write_file,truncate_file,sync_file,size_file,lock_file,unlock_file,check_file,control_file,sector_file,device_file,shm_map,shm_lock,shm_barrier,shm_unmap,fetch_file,unfetch_file};
static sqlite3_io_methods basic_methods={1,close_file,read_file,write_file,truncate_file,sync_file,size_file,lock_file,unlock_file,check_file,control_file,sector_file,device_file};
/* SUBJOURNAL 仅供活事务回滚使用，真实 I/O 错误上抛，不归入持久文件前缀。 */
static int ephemeral_close(sqlite3_file *p){int rc=close_file(p);atomic_fetch_add(&ephemeral_closes,1);return rc;}
static int ephemeral_read(sqlite3_file *p,void *b,int n,sqlite3_int64 o){atomic_fetch_add(&ephemeral_reads,1);return M(p)->xRead(R(p),b,n,o);}
static int ephemeral_write(sqlite3_file *p,const void *b,int n,sqlite3_int64 o){
 atomic_fetch_add(&ephemeral_writes,1);
 if(atomic_load(&ephemeral_write_error)){atomic_fetch_add(&ephemeral_failures,1);return SQLITE_IOERR_WRITE;}
 return M(p)->xWrite(R(p),b,n,o);
}
static int ephemeral_truncate(sqlite3_file *p,sqlite3_int64 n){return M(p)->xTruncate(R(p),n);}
static int ephemeral_sync(sqlite3_file *p,int flags){ephemeral_io++;int rc=M(p)->xSync(R(p),flags);ephemeral_io--;return rc;}
static int ephemeral_sector(sqlite3_file *p){return M(p)->xSectorSize(R(p));}
static int ephemeral_device(sqlite3_file *p){return M(p)->xDeviceCharacteristics(R(p));}
static sqlite3_io_methods ephemeral_methods={1,ephemeral_close,ephemeral_read,ephemeral_write,ephemeral_truncate,ephemeral_sync,size_file,lock_file,unlock_file,check_file,control_file,ephemeral_sector,ephemeral_device};
static int open_file(sqlite3_vfs *v,const char *name,sqlite3_file *file,int flags,int *out){
 TraceFile *p=F(file); memset(p,0,sizeof(*p)); p->real=sqlite3_malloc(parent->szOsFile); if(!p->real)return SQLITE_NOMEM;
 memset(p->real,0,parent->szOsFile); int rc=parent->xOpen(parent,name,p->real,flags,out);
 if(rc!=SQLITE_OK){if(p->real->pMethods)p->real->pMethods->xClose(p->real);sqlite3_free(p->real);p->real=0;return rc;}
 if((flags&SQLITE_OPEN_MAIN_DB) && (p->real->pMethods->iVersion<2 || !p->real->pMethods->xShmMap)){p->real->pMethods->xClose(p->real);sqlite3_free(p->real);return SQLITE_CANTOPEN;}
 const int subjournal=SQLITE_OPEN_SUBJOURNAL|SQLITE_OPEN_READWRITE|SQLITE_OPEN_CREATE|SQLITE_OPEN_DELETEONCLOSE|SQLITE_OPEN_EXCLUSIVE;
 if(name==0 && flags==subjournal){atomic_fetch_add(&ephemeral_opens,1);p->file.pMethods=&ephemeral_methods;return SQLITE_OK;}
 if(flags&SQLITE_OPEN_MAIN_DB){
  if(!name || (database_path && strcmp(database_path,name)!=0)){close_file(&p->file);return SQLITE_CANTOPEN;}
  if(!database_path){database_path=sqlite3_mprintf("%s",name);if(!database_path){close_file(&p->file);return SQLITE_NOMEM;}}
 }
 p->id=(flags&SQLITE_OPEN_MAIN_DB)?0:(flags&SQLITE_OPEN_WAL)?1:2;
 p->dirsync=(flags&SQLITE_OPEN_CREATE)!=0;
 if(!(flags&(SQLITE_OPEN_MAIN_DB|SQLITE_OPEN_WAL|SQLITE_OPEN_MAIN_JOURNAL))) {p->real->pMethods->xClose(p->real);sqlite3_free(p->real);return SQLITE_CANTOPEN;}
 pthread_mutex_lock(&mutex);record('O',p->id,0,0,0);pthread_mutex_unlock(&mutex); p->file.pMethods=(flags&SQLITE_OPEN_MAIN_DB)?&methods:&basic_methods; return SQLITE_OK;
}
static int delete_file(sqlite3_vfs *v,const char *name,int syncdir){
 pthread_mutex_lock(&mutex);int rc=parent->xDelete(parent,name,syncdir);
 if(rc==SQLITE_OK && name && database_path) {
  size_t n=strlen(database_path);int id=-1;
  if(strcmp(name,database_path)==0)id=0;
  else if(strncmp(name,database_path,n)==0 && strcmp(name+n,"-wal")==0)id=1;
  else if(strncmp(name,database_path,n)==0 && strcmp(name+n,"-journal")==0)id=2;
  if(id>=0){record('D',id,syncdir,0,0);if(syncdir)record('N',id,0,0,0);}
 }
 pthread_mutex_unlock(&mutex);return rc;
}
int register_model(const char *path){
 if(trace)return SQLITE_MISUSE;
 parent=sqlite3_vfs_find(0);if(!parent)return SQLITE_ERROR;
 #ifdef __APPLE__
 if(parent->iVersion<3 || !parent->xGetSystemCall || !parent->xSetSystemCall)return SQLITE_NOTFOUND;
 real_fcntl=(int(*)(int,int,...))parent->xGetSystemCall(parent,"fcntl");
 if(!real_fcntl || parent->xSetSystemCall(parent,"fcntl",(sqlite3_syscall_ptr)observed_fcntl)!=SQLITE_OK)return SQLITE_NOTFOUND;
 #endif
 #ifdef __APPLE__
 real_fsync=(int(*)(int))parent->xGetSystemCall(parent,"fsync");
 if(!real_fsync || parent->xSetSystemCall(parent,"fsync",(sqlite3_syscall_ptr)observed_fsync)!=SQLITE_OK)return SQLITE_NOTFOUND;
 #endif
 trace=fopen(path,"wb");if(!trace)return SQLITE_CANTOPEN;
 wrapper=*parent;wrapper.zName="lerna-storage-model";wrapper.szOsFile=sizeof(TraceFile);wrapper.xOpen=open_file;wrapper.xDelete=delete_file;wrapper.pNext=0;
 return sqlite3_vfs_register(&wrapper,1);
}
uint64_t model_sequence(void){pthread_mutex_lock(&mutex);uint64_t n=sequence;pthread_mutex_unlock(&mutex);return n;}
void model_mode(int m,int skip){pthread_mutex_lock(&mutex);mode=m;skip_sync=skip;pthread_mutex_unlock(&mutex);}

uint64_t model_native_attempts(void){return atomic_load(&native_attempts);}
uint64_t model_native_successes(void){return atomic_load(&native_successes);}
uint64_t model_native_failures(void){return atomic_load(&native_failures);}
const char *model_source_id(void){return sqlite3_sourceid();}

uint64_t model_directory_failures(void){return atomic_load(&directory_failures);}

uint64_t model_ephemeral_opens(void){return atomic_load(&ephemeral_opens);}
uint64_t model_ephemeral_reads(void){return atomic_load(&ephemeral_reads);}
uint64_t model_ephemeral_writes(void){return atomic_load(&ephemeral_writes);}
uint64_t model_ephemeral_closes(void){return atomic_load(&ephemeral_closes);}
uint64_t model_ephemeral_failures(void){return atomic_load(&ephemeral_failures);}
void model_ephemeral_write_error(int enabled){atomic_store(&ephemeral_write_error,enabled);}
