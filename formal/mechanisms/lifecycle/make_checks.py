import json,pathlib
p=pathlib.Path(__file__).parent
checks=[]
def cfg(id,model,constants,invs,exit=0,contains=None):
    path=id+'.cfg'
    (p/path).write_text('SPECIFICATION Spec\nCONSTANTS\n'+''.join('  '+k+' = '+v+'\n' for k,v in constants.items())+'INVARIANTS\n'+''.join('  '+v+'\n' for v in invs)+'CHECK_DEADLOCK FALSE\n')
    checks.append({'id':id,'kind':'tlc','model':model+'.tla','config':path,'expected_exit':exit,'contains':contains or 'Model checking completed. No error has been found.'})
def suite(prefix,model,constants,invs,bugs,witnesses):
    cfg(prefix+'-safe',model,constants,invs)
    for name,bug,prop in bugs:
        mutated=constants|{bug:'TRUE'}
        cfg(prefix+'-'+name,model,mutated,[prop],12,'Invariant '+prop+' is violated.')
    for name,prop in witnesses:
        cfg(prefix+'-'+name,model,constants,[prop],12,'Invariant '+prop+' is violated.')
suite('refs','References',{'Refs':'{r1,r2}','BugStaleRegister':'FALSE','BugEarlyRelease':'FALSE'},['TypeOK','UseHasPin','NoPrematureReclaim','FreshRegistration'],[('stale','BugStaleRegister','FreshRegistration'),('early','BugEarlyRelease','UseHasPin')],[('normal','NoNormalWitness'),('recovery','NoRecoveryWitness')])
suite('ui','UI',{'BugStaleReply':'FALSE','BugDeltaBase':'FALSE'},['TypeOK','DisplayAuthority','DeltaContinuity','ResultPersistence'],[('stale','BugStaleReply','DisplayAuthority'),('delta','BugDeltaBase','DeltaContinuity')],[('normal','NoNormalWitness'),('recovery','NoRecoveryWitness')])
suite('eval','Evaluation',{'Targets':'{t1,t2}','BugStaleQualification':'FALSE','BugVersionBinding':'FALSE','BugTargetGate':'FALSE'},['TargetQualification','EvidenceSeparation','ExactFreshQualification','DurableResponsibility','HistoryPreserved'],[('stale','BugStaleQualification','ExactFreshQualification'),('version','BugVersionBinding','ExactFreshQualification'),('target','BugTargetGate','TargetQualification')],[('normal','NoNormalWitness'),('recovery','NoRecoveryWitness'),('partial','NoPartialWitness')])
suite('input','Input',{'BugSendBeforeMapping':'FALSE','BugSecondConsumer':'FALSE','BugOldMeaning':'FALSE','SplitMapping':'TRUE'},['MappingBeforeForward','OneConsumer','MeaningBound','NoParentRegression'],[('unmapped','BugSendBeforeMapping','MappingBeforeForward'),('duplicate','BugSecondConsumer','OneConsumer'),('meaning','BugOldMeaning','MeaningBound')],[('normal','NoNormalWitness'),('recovery','NoRecoveryWitness')])
suite('rollback','Rollback',{'BugStaleRestore':'FALSE','BugDisableAsRestore':'FALSE','BugRewindFacts':'FALSE'},['CurrentRestoreBasis','RestoredRequiresActivation','NoFactRewind','DispositionHonest'],[('stale','BugStaleRestore','CurrentRestoreBasis'),('false-restored','BugDisableAsRestore','RestoredRequiresActivation'),('rewind','BugRewindFacts','NoFactRewind')],[('normal','NoNormalWitness'),('blocked','NoBlockedWitness'),('recovery','NoRecoveryWitness')])
suite('preview','Preview',{'BugMissingPreview':'FALSE','BugStalePreview':'FALSE'},['RetainedBeforeAcquisition','CompleteCurrentPreview'],[('missing','BugMissingPreview','CompleteCurrentPreview'),('stale','BugStalePreview','CompleteCurrentPreview')],[('normal','NoNormalWitness'),('closed','NoClosedOldPacketWitness')])
for ident,spec,code in [('refs-live','FairSpec',0),('refs-unfair','Spec',13)]:
    (p/(ident+'.cfg')).write_text('SPECIFICATION '+spec+'\nCONSTANTS\n  Refs = {r1,r2}\n  BugStaleRegister = FALSE\n  BugEarlyRelease = FALSE\nPROPERTY EventualReclaim\nCHECK_DEADLOCK FALSE\n')
    checks.append({'id':ident,'kind':'tlc','model':'References.tla','config':ident+'.cfg','expected_exit':code,'contains':'Model checking completed. No error has been found.' if code==0 else 'Temporal properties were violated.'})
checks += [
  {'id':'refs-proof','kind':'lean','source':'References.lean','expected_exit':0,'contains':"'ReferenceLifecycle.reachable_safe' depends on axioms: [propext, Quot.sound]"},
  {'id':'eval-rules-proof','kind':'lean','source':'EvaluationRules.lean','expected_exit':0,'contains':"'EvaluationRules.reachable_inv' depends on axioms: [propext, Quot.sound]"}
]
(p/'checks.json').write_text(json.dumps({'checks':checks},indent=2)+'\n')
