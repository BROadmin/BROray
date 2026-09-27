/* Exact bounded coordinator closure, from authenticated CODE_ROOT only.
 * Retention is not an executor admission and never launches this code. */
#define RC_FILES 16
static const char *rc_paths[RC_FILES]={
    "bin/broray-ops-guard","lib/operation-client.sh","lib/operation-coordinator.sh",
    "lib/operation-owner.sh","lib/operation-journal.sh","lib/operation-report.sh",
    "lib/operation-report-facts.sh","lib/operation-publication.sh","lib/operation-route-recovery.sh",
    "lib/operation-platform-recovery.sh","lib/operation-platform-service.sh",
    "lib/operation-platform-generation.sh","lib/operation-platform-bootguard.sh",
    "lib/operation-public.jq","lib/operation-report-public.jq",
    "lib/operation-scheduling.sh"
};
static void rc_free(struct migration_file files[RC_FILES]){for(int i=0;i<RC_FILES;i++)free(files[i].bytes);}
static int rc_load(int root,struct migration_file files[RC_FILES],char **manifest,size_t *size,char sha[65]){
    FILE *f=open_memstream(manifest,size);if(!f)return -1;int bad=0;size_t total=0;
    fputs("BROray-recovery-code-files/1\n",f);
    for(int i=0;i<RC_FILES;i++){
        if(migration_read(root,rc_paths[i],&files[i],0)||!files[i].size||files[i].size>2097152||!(files[i].mode&0400)||
           (i==0&&!(files[i].mode&0100))){bad=1;break;}
        total+=files[i].size;if(total>4194304){bad=1;break;}
        fprintf(f,"%04o\t%s\t%s\n",files[i].mode,files[i].sha,rc_paths[i]);
    }
    if(fclose(f))bad=1;if(bad)return -1;digest_bytes(*manifest,*size,sha);return 0;
}
static int rc_exact(int root,const struct migration_file expected[RC_FILES]){
    for(int i=0;i<RC_FILES;i++)if(bg_exact(root,rc_paths[i],expected[i].bytes,expected[i].size,expected[i].mode))return -1;return 0;
}
static int rc_names(int root,const char *const *names,unsigned count){
    DIR *d=directory_stream(root);if(!d||count>30){if(d)closedir(d);return -1;}
    struct dirent *e;unsigned seen=0;int bad=0;errno=0;
    while((e=readdir(d))){if(!strcmp(e->d_name,".")||!strcmp(e->d_name,".."))continue;unsigned bit=0;
        for(unsigned i=0;i<count;i++)if(!strcmp(e->d_name,names[i]))bit=1U<<i;
        if(!bit||(seen&bit)){bad=1;break;}seen|=bit;errno=0;
    }
    if(!e&&errno)bad=1;closedir(d);return bad||seen!=((1U<<count)-1U)?-1:0;
}
static int rc_complete(int base,int code,int bin,int lib){
    const char *root_names[]={"manifest.record","staged.receipt","code"},*code_names[]={"bin","lib"},*bin_names[]={"broray-ops-guard"};
    const char *lib_names[RC_FILES-1];for(int i=1;i<RC_FILES;i++)lib_names[i-1]=rc_paths[i]+4;
    return rc_names(base,root_names,3)||rc_names(code,code_names,2)||rc_names(bin,bin_names,1)||rc_names(lib,lib_names,RC_FILES-1)?-1:0;
}
static int recovery_code_impl(int argc,char **argv,int emit){
    /* stage OP LIVE MIGRATION SHA SOURCE; verify OP LIVE MIGRATION SHA */
    int verify=argc>1&&!strcmp(argv[1],"recovery-code-verify");
    if(argc!=(verify?6:7)||!migration_path(argv[2])||!migration_path(argv[3])||!migration_path(argv[4])||!hex64(argv[5])||(!verify&&!migration_path(argv[6])))return 64;
    int op=-1,mig=-1,src=-1,base=-1,code=-1,bin=-1,lib=-1,result=75,prior=0;umask(077);
    struct bg_input input;memset(&input,0,sizeof input);struct migration_file files[RC_FILES];memset(files,0,sizeof files);
    char *manifest=NULL;size_t size=0;char manifest_sha[65],native[65],guard[768],guard_sha[65],binding[1024],receipt[128];
    char path[PATH_MAX],directory[PATH_MAX],codepath[PATH_MAX],binpath[PATH_MAX],libpath[PATH_MAX];
    if(snprintf(path,sizeof path,"%s/platform-migration",argv[2])>=(int)sizeof path||strcmp(path,argv[4])||
       snprintf(directory,sizeof directory,"%s/platform-recovery-code",argv[2])>=(int)sizeof directory||
       snprintf(codepath,sizeof codepath,"%s/code",directory)>=(int)sizeof codepath||
       snprintf(binpath,sizeof binpath,"%s/bin",codepath)>=(int)sizeof binpath||snprintf(libpath,sizeof libpath,"%s/lib",codepath)>=(int)sizeof libpath)goto done;
    op=checked_directory(argv[2]);mig=checked_directory(argv[4]);
    if(op<0||mig<0||flock(op,LOCK_EX|LOCK_NB)||flock(mig,LOCK_SH|LOCK_NB)||migration_boot(boot)||peer_executable_hash(getpid(),native)||bg_source(mig,argv[4],argv[3],argv[5],&input))goto done;
    if(!verify&&strcmp(boot,input.old_boot))goto done;
    const char *name=strrchr(argv[2],'/');if(!name||strcmp(name+1,input.operation))goto done;
    int gn=bg_binding_text(guard,&input,argv[5],native);if(gn<0||bg_bound_exact(op,guard,(size_t)gn))goto done;digest_bytes(guard,(size_t)gn,guard_sha);
    prior=bg_exists(op,"platform-recovery-code.json");if(prior<0||(verify&&!prior)||(!prior&&bg_exists(op,"platform-recovery-code")!=0))goto done;
    src=verify?checked_directory(codepath):migration_directory(argv[6]);
    if(src<0||rc_load(src,files,&manifest,&size,manifest_sha))goto done;
    int bn=snprintf(binding,sizeof binding,"{\"schemaVersion\":1,\"contract\":\"broray-recovery-code/1\",\"operationId\":\"%s\",\"stopNonce\":\"%s\",\"migrationIntentSha256\":\"%s\",\"nativeSha256\":\"%s\",\"bootGuardBindingSha256\":\"%s\",\"codeManifestSha256\":\"%s\",\"processAuthority\":false,\"activationAllowed\":false}\n",input.operation,input.nonce,argv[5],native,guard_sha,manifest_sha);
    int rn=snprintf(receipt,sizeof receipt,"BROray-recovery-code-staged/1\n%s\n",manifest_sha);
    if(bn<0||bn>=(int)sizeof binding||rn<0||rn>=(int)sizeof receipt||bg_bound_exact(op,guard,(size_t)gn)||rc_exact(src,files)||
       migration_record(op,"platform-recovery-code.json",binding,(size_t)bn,!prior))goto done;
    if(!prior&&(mkdirat(op,"platform-recovery-code",0700)||fsync(op)))goto done;
    base=checked_directory(directory);if(base<0||flock(base,LOCK_EX|LOCK_NB))goto done;
    if(!prior&&(bg_empty(base)||mkdirat(base,"code",0700)||fsync(base)))goto done;
    code=checked_directory(codepath);if(code<0)goto done;
    if(!prior&&(bg_empty(code)||mkdirat(code,"bin",0700)||mkdirat(code,"lib",0700)||fsync(code)))goto done;
    bin=checked_directory(binpath);lib=checked_directory(libpath);if(bin<0||lib<0)goto done;
    if(prior?rc_complete(base,code,bin,lib):(bg_empty(bin)||bg_empty(lib)))goto done;
    if(migration_record(base,"manifest.record",manifest,size,!prior))goto done;
    for(int i=0;i<RC_FILES;i++){
        int fd=i?lib:bin;const char *shortname=rc_paths[i]+4;
        if((!prior&&bg_candidate(fd,shortname,files[i].bytes,files[i].size,files[i].mode))||bg_exact(fd,shortname,files[i].bytes,files[i].size,files[i].mode)||bg_sync_file(fd,shortname,files[i].mode))goto done;
    }
    if(rc_exact(src,files)||bg_bound_exact(op,guard,(size_t)gn)||bg_record_exact(op,"platform-recovery-code.json",binding,(size_t)bn)||
       bg_record_exact(base,"manifest.record",manifest,size)||rc_exact(code,files)||migration_record(base,"staged.receipt",receipt,(size_t)rn,!prior)||rc_complete(base,code,bin,lib))goto done;
    if(migration_sync_directory(binpath,bin)||migration_sync_directory(libpath,lib)||migration_sync_directory(codepath,code)||migration_sync_directory(directory,base))goto done;
    if(rc_complete(base,code,bin,lib)||rc_exact(code,files)||rc_exact(src,files)||bg_record_exact(base,"manifest.record",manifest,size)||
       bg_record_exact(base,"staged.receipt",receipt,(size_t)rn)||bg_record_exact(op,"platform-recovery-code.json",binding,(size_t)bn)||bg_bound_exact(op,guard,(size_t)gn))goto done;
    if(emit){printf("{\"ok\":true,\"phase\":\"%s\",\"codeManifestSha256\":\"%s\",\"codeRoot\":",verify?"RECOVERY_CODE_VERIFIED":"RECOVERY_CODE_STAGED",manifest_sha);json_string(stdout,codepath);
        puts(",\"processAuthority\":false,\"activationAllowed\":false}");}result=0;
done:
    if(op>=0)close(op);if(mig>=0)close(mig);if(src>=0)close(src);if(base>=0)close(base);if(code>=0)close(code);if(bin>=0)close(bin);if(lib>=0)close(lib);
    rc_free(files);free(input.intent);free(manifest);return result?migration_error("RECOVERY_CODE_EVIDENCE_UNCONFIRMED"):0;
}
static int recovery_code_main(int argc,char **argv){return recovery_code_impl(argc,argv,1);}

static int platform_start_intent_apply(char **argv,int op,const char *op_path,const struct bg_input *input,const char *native,const struct identity *executor,int held,int statefd,const char *shell,const char *controller,int observe);
static int platform_start_apply(char **argv,int op,const char *op_path,const struct bg_input *input,const char *native,const struct identity *executor,int held,int statefd,const char *shell,const char *controller);
static int platform_commit_apply(char **argv,int op,const char *op_path,const struct bg_input *input,const char *native,const struct identity *executor,int held,int statefd,const char *shell,const char *controller);
static int platform_stop_current_apply(char **argv,int op,const char *op_path,const struct bg_input *input,const char *native,const struct identity *executor,int held,int statefd,const char *shell,const char *controller);
static int platform_preserve_apply(char **argv,int op,const char *op_path,const struct bg_input *input,const char *native,const struct identity *executor,int held,int statefd,const char *shell,const char *controller);
static int platform_retry_prepare_apply(char **argv,int op,const char *op_path,const struct bg_input *input,const char *native,const struct identity *executor,int held,int statefd,const char *shell,const char *controller);
static int platform_retained_proof(int op,const struct bg_input *input,const char *migration,const char *native);
static int recovery_context_proof(char **argv,int held,int statefd,const char *op,const char *shell,const char *controller,const struct bg_input *input){
    int pipes[2]={-1,-1},status=0,result=-1;pid_t child=-1;struct stat lock,named;
    int completing=!strcmp(argv[1],"recovery-commit-check");
    int installing=!strcmp(argv[1],"recovery-install")||!strcmp(argv[1],"recovery-rollback")||!strcmp(argv[1],"recovery-start-intent")||!strcmp(argv[1],"recovery-start")||!strcmp(argv[1],"recovery-commit")||!strcmp(argv[1],"recovery-stop-current")||!strcmp(argv[1],"recovery-preserve")||!strcmp(argv[1],"recovery-retry");
    char proof[2048],expected[1024],migration[PATH_MAX];
    if(fstat(held,&lock)||fstatat(statefd,"operations.guard",&named,AT_SYMLINK_NOFOLLOW)||
       lock.st_dev!=named.st_dev||lock.st_ino!=named.st_ino||named.st_nlink!=1||!S_ISREG(named.st_mode)||
       (named.st_mode&07777)!=0600||named.st_uid!=geteuid()||
       snprintf(migration,sizeof migration,"%s/platform-migration",op)>=(int)sizeof migration)goto done;
    int evidence=checked_directory(op);char native[65];
    if(evidence<0)goto done;
    int evidence_bad=peer_executable_hash(getpid(),native)||platform_attempt_load(evidence,op,argv[2],input,argv[4],native)||
        platform_retained_proof(platform_attempt_current(evidence),input,argv[4],native);
    close(evidence);if(evidence_bad){migration_error("PLATFORM_RETAINED_EVIDENCE_UNCONFIRMED");goto done;}
    char *verify[]={argv[0],"recovery-code-verify",(char *)op,argv[2],migration,argv[4],NULL};
    /* Never execute a changed retained script while rechecking a mutation. */
    if(recovery_code_impl(6,verify,0)||pipe2(pipes,O_CLOEXEC))goto done;
    child=fork();if(child<0)goto done;
    if(!child){close(pipes[0]);if(dup2(pipes[1],STDOUT_FILENO)<0)_exit(74);close(pipes[1]);
        char *command[]={(char *)shell,(char *)controller,completing?"platform-preflight-complete-context":installing?"platform-preflight-install-context":"platform-preflight-boot-context",argv[3],argv[5],NULL};execv(shell,command);_exit(74);}
    close(pipes[1]);pipes[1]=-1;size_t used=0;
    while(used<sizeof proof-1){ssize_t n=read(pipes[0],proof+used,sizeof proof-1-used);if(n<0&&errno==EINTR)continue;if(n<0)goto done;if(!n)break;used+=(size_t)n;}
    close(pipes[0]);pipes[0]=-1;proof[used]=0;
    pid_t waited;do{waited=waitpid(child,&status,0);}while(waited<0&&errno==EINTR);
    if(waited!=child||!WIFEXITED(status)||WEXITSTATUS(status)||used>=sizeof proof-1||memchr(proof,0,used))goto done;
    int n=snprintf(expected,sizeof expected,"{\"ok\":true,\"phase\":\"%s\",\"operationId\":\"%s\",\"oldBootId\":\"%s\",\"currentBootId\":\"%s\",\"oldBootEnded\":true,\"serviceStopped\":false,\"signalsAuthorized\":false,\"activationAllowed\":false,\"executorAuthorized\":false,\"platformReady\":false}\n",completing?"COMPLETE_CONTEXT_VERIFIED":installing?"INSTALL_CONTEXT_VERIFIED":"BOOT_CONTEXT_VERIFIED",argv[3],input->old_boot,boot);
    if(n<0||n>=(int)sizeof expected||used!=(size_t)n||memcmp(proof,expected,used)||
       fstatat(statefd,"operations.guard",&named,AT_SYMLINK_NOFOLLOW)||lock.st_dev!=named.st_dev||lock.st_ino!=named.st_ino||named.st_nlink!=1||!S_ISREG(named.st_mode)||(named.st_mode&07777)!=0600||named.st_uid!=geteuid())goto done;
    result=0;
done:
    if(pipes[0]>=0)close(pipes[0]);if(pipes[1]>=0)close(pipes[1]);return result;
}

/* The admitted executor is this bounded native process. The shell below is
 * read-only and inherits serialization; it never becomes a platform writer.
 * No authority is transferred to a PID, token, shell, or later invocation. */
static int recovery_boot_admit(char **argv,int held,int statefd,const char *op,
                               const char *shell,const char *controller){
    int base=-1,mig=-1,result=75;struct bg_input input;
    memset(&input,0,sizeof input);char path[PATH_MAX],native[65];
    char *record=NULL,*prior=NULL;size_t size=0,prior_size=0;struct identity executor,again;
    struct migration_file owner,binding;memset(&owner,0,sizeof owner);memset(&binding,0,sizeof binding);
    struct stat lock,named;int replay=0;umask(077);
    base=checked_directory(op);
    if(snprintf(path,sizeof path,"%s/platform-migration",op)>=(int)sizeof path)goto done;
    mig=checked_directory(path);
    if(base<0||mig<0||migration_boot(boot)||bg_source(mig,path,argv[2],argv[4],&input)||
       !strcmp(boot,input.old_boot)||strcmp(input.nonce,argv[5])||strcmp(input.operation,argv[3])||
       peer_executable_hash(getpid(),native)||capture(getpid(),&executor))goto done;
    /* A completed read-only child reply is necessary but not sufficient:
     * recheck the held lock and all bound input records before publication. */
    /* Stop performs the same full context proof in its initial read-only
     * platform_install_apply, before publishing STOP_INTENT or sending STOP,
     * and again before its terminal receipt. No other entry may defer this
     * admission. Native identity/input reads below grant no signal authority. */
    if(strcmp(argv[1],"recovery-stop-current")&&recovery_context_proof(argv,held,statefd,op,shell,controller,&input))goto done;
    if(fstat(held,&lock)||fstatat(statefd,"operations.guard",&named,AT_SYMLINK_NOFOLLOW)||
       lock.st_dev!=named.st_dev||lock.st_ino!=named.st_ino||named.st_nlink!=1||!S_ISREG(named.st_mode)||
       (named.st_mode&07777)!=0600||named.st_uid!=geteuid()||
       migration_read(base,"owner.json",&owner,0)||owner.mode!=0600||
       migration_read(base,"platform-bootguard.json",&binding,0)||binding.mode!=0600)goto done;
    if(!strcmp(argv[1],"recovery-stop-current")&&platform_attempt_load(base,op,argv[2],&input,argv[4],native))goto done;
    int launch_op=platform_attempt_current(base);if(launch_op<0)goto done;
    if(!strcmp(argv[1],"recovery-retire")){
        result=legacy_retirement_apply(argv,base,op,&input,native,&executor,held,statefd,shell,controller,0);goto done;
    }
    if(!strcmp(argv[1],"recovery-backup")){
        result=platform_backup_apply(argv,base,op,&input,native,&executor,held,statefd,shell,controller);goto done;
    }
    if(!strcmp(argv[1],"recovery-install")){
        result=platform_install_apply(argv,base,op,&input,native,&executor,held,statefd,shell,controller,0);goto done;
    }
    if(!strcmp(argv[1],"recovery-rollback")){
        if(platform_attempt_current(base)!=platform_context_op(base))goto done;
        result=platform_rollback_apply(argv,base,op,&input,native,&executor,held,statefd,shell,controller);goto done;
    }
    if(!strcmp(argv[1],"recovery-retry")){
        result=platform_retry_prepare_apply(argv,launch_op,op,&input,native,&executor,held,statefd,shell,controller);goto done;
    }
    if(!strcmp(argv[1],"recovery-preserve")){
        result=platform_preserve_apply(argv,launch_op,op,&input,native,&executor,held,statefd,shell,controller);goto done;
    }
    if(!strcmp(argv[1],"recovery-start-intent")){
        result=platform_start_intent_apply(argv,launch_op,op,&input,native,&executor,held,statefd,shell,controller,0);goto done;
    }
    if(!strcmp(argv[1],"recovery-start")){
        result=platform_start_apply(argv,launch_op,op,&input,native,&executor,held,statefd,shell,controller);goto done;
    }
    if(!strcmp(argv[1],"recovery-commit")||!strcmp(argv[1],"recovery-commit-check")){
        result=platform_commit_apply(argv,launch_op,op,&input,native,&executor,held,statefd,shell,controller);goto done;
    }
    if(!strcmp(argv[1],"recovery-stop-current")){
        result=platform_stop_current_apply(argv,launch_op,op,&input,native,&executor,held,statefd,shell,controller);goto done;
    }
    int existed=bg_exists(base,"platform-boot-admission.json"),anchored=bg_exists(base,"platform-boot-admission.anchor");
    if(existed<0||anchored<0||existed!=anchored)goto done;
    if(existed){
        /* Replay validates every byte. PID/startTicks here identify historic
         * evidence only; they are never used for a signal or present owner. */
        if(safe_bytes_at(base,"platform-boot-admission.json",&prior,&prior_size)||memchr(prior,0,prior_size))goto done;
        const char *mark=strstr(prior,"\"executor\":{\"pid\":");long pid=0;unsigned long long start=0;int consumed=0;
        if(!mark||sscanf(mark,"\"executor\":{\"pid\":%ld,\"startTicks\":\"%llu\"%n",&pid,&start,&consumed)!=2||pid<=1||pid>INT_MAX||!start||!consumed)goto done;
        executor.pid=(pid_t)pid;executor.ticks=start;replay=1;
    }
    FILE *f=open_memstream(&record,&size);if(!f)goto done;
    fprintf(f,"{\"schemaVersion\":1,\"contract\":\"broray-boot-executor/1\",\"operationId\":\"%s\",\"stopNonce\":\"%s\",\"oldBootId\":\"%s\",\"bootId\":\"%s\",\"migrationIntentSha256\":\"%s\",\"nativeSha256\":\"%s\",\"originalOwnerSha256\":\"%s\",\"bootGuardBindingSha256\":\"%s\",\"executionModel\":\"bounded-native-under-inherited-flock\",\"executor\":",argv[3],argv[5],input.old_boot,boot,argv[4],native,owner.sha,binding.sha);
    identity_json(f,&executor);fputs(",\"signalsAuthorized\":false,\"serviceStopped\":false,\"activationAllowed\":false}\n",f);
    if(fclose(f)|| (replay&&(size!=prior_size||memcmp(record,prior,size))))goto done;
    char admission_sha[65],admission_anchor[128];digest_bytes(record,size,admission_sha);
    int an=snprintf(admission_anchor,sizeof admission_anchor,"BROray-boot-executor-anchor/1\n%s\n",admission_sha);
    if(an<0||an>=(int)sizeof admission_anchor||
       (!replay&&(capture(getpid(),&again)||!identity_equal(&executor,&again)))||
       bg_exact(base,"owner.json",owner.bytes,owner.size,0600)||bg_exact(base,"platform-bootguard.json",binding.bytes,binding.size,0600)||
       migration_record(base,"platform-boot-admission.anchor",admission_anchor,(size_t)an,!replay)||
       migration_record(base,"platform-boot-admission.json",record,size,!replay)||
       bg_record_exact(base,"platform-boot-admission.anchor",admission_anchor,(size_t)an)||
       bg_record_exact(base,"platform-boot-admission.json",record,size)||fsync(base))goto done;
    printf("{\"ok\":true,\"phase\":\"BOOT_EXECUTOR_RECORDED\",\"replayed\":%s,\"serviceStopped\":false,\"activationAllowed\":false,\"transferableAuthority\":false}\n",replay?"true":"false");result=0;
done:
    if(base>=0)close(base);if(mig>=0)close(mig);
    free(input.intent);free(owner.bytes);free(binding.bytes);free(record);free(prior);
    return result?migration_error("BOOT_EXECUTOR_ADMISSION_UNCONFIRMED"):0;
}

/* A canonical observational entry, not boot executor admission. It never
 * creates the coordinator lock and never accepts environment-selected code,
 * fake /proc, test identities, service paths or a caller-selected shell.
 * The existing flock description remains held across the shell exec. */
/* A read-only proof for the coordinator must share its inherited flock.
 * Never open a second descriptor and mistake lock contention for authority.
 * Taking EX on the inherited open description preserves it in the parent. */
static int recovery_inherited_guard(int base){
    struct stat named,st;int selected=-1,result=-1;
    if(fstatat(base,"operations.guard",&named,AT_SYMLINK_NOFOLLOW)||!S_ISREG(named.st_mode)||
       named.st_nlink!=1||named.st_uid!=geteuid()||(named.st_mode&07777)!=0600)return -1;
    DIR *fds=opendir("/proc/self/fd");if(!fds)return -1;struct dirent *entry;
    while((entry=readdir(fds))){
        char *end=NULL;errno=0;long number=strtol(entry->d_name,&end,10);
        if(errno||!end||*end||number<3||number>INT_MAX||number==dirfd(fds))continue;
        int fd=(int)number,flags=fcntl(fd,F_GETFL);
        if(flags<0||(flags&O_ACCMODE)!=O_RDWR||fstat(fd,&st)||st.st_dev!=named.st_dev||st.st_ino!=named.st_ino)continue;
        selected=fd;break;
    }
    closedir(fds);
    if(selected>=0&&!flock(selected,LOCK_EX|LOCK_NB))result=dup(selected);
    return result;
}

static int recovery_inspect_main(int argc,char **argv){
    /* recovery-inspect LIVE_ROOT OPERATION_ID MIGRATION_SHA STOP_NONCE */
    int retry=!strcmp(argv[1],"recovery-retry");
    if(argc!=(retry?7:6)||!migration_path(argv[2])||!token(argv[3],96)||!hex64(argv[4])||!token(argv[5],32)||strlen(argv[5])!=32||(retry&&!hex64(argv[6])))return 64;
    char canonical[PATH_MAX],app[PATH_MAX],state[PATH_MAX],op[PATH_MAX],migration[PATH_MAX],code[PATH_MAX];
    char lock[PATH_MAX],global[PATH_MAX],updater[PATH_MAX],legacy[PATH_MAX],ram[PATH_MAX],shell[PATH_MAX],guard[PATH_MAX],controller[PATH_MAX],path[PATH_MAX+80];
    int root=-1,base=-1,held=-1;struct stat before,after;
    if(!realpath(argv[2],canonical)||strcmp(canonical,argv[2]))goto fail;
    root=migration_directory(argv[2]);if(root<0)goto fail;
    const char *prefix=strcmp(argv[2],"/")?argv[2]:"";
#define RI_PATH(dest,fmt,...) do{int n=snprintf(dest,sizeof dest,fmt,__VA_ARGS__);if(n<0||n>=(int)sizeof dest)goto fail;}while(0)
    RI_PATH(app,"%s/opt/broray",prefix);RI_PATH(state,"%s/opt/var/lib/broray",prefix);
    RI_PATH(op,"%s/operations/%s",state,argv[3]);RI_PATH(migration,"%s/platform-migration",op);
    RI_PATH(code,"%s/platform-recovery-code/code",op);RI_PATH(lock,"%s/operations.guard",state);
    RI_PATH(global,"%s/opt/var/lock/broray/global-operation.lock",prefix);RI_PATH(updater,"%s/opt/var/lib/broray-updater",prefix);
    RI_PATH(legacy,"%s/tmp/broray-global-operation.lock",prefix);RI_PATH(ram,"%s/tmp/broray-operations",prefix);
    RI_PATH(shell,"%s/opt/bin/ash",prefix);RI_PATH(guard,"%s/bin/broray-ops-guard",code);RI_PATH(controller,"%s/lib/operation-coordinator.sh",code);
    RI_PATH(path,"%s/opt/bin:%s/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin",prefix,prefix);
#undef RI_PATH
    base=checked_directory(state);if(base<0)goto fail;
    held=(!strcmp(argv[1],"recovery-commit-check")||!strcmp(argv[1],"recovery-service-stop"))?recovery_inherited_guard(base):-1;
    if(held<0&&strcmp(argv[1],"recovery-commit-check"))held=openat(base,"operations.guard",O_RDWR|O_NOFOLLOW|O_CLOEXEC);
    if(held<0||fstat(held,&before)||!S_ISREG(before.st_mode)||before.st_uid!=geteuid()||before.st_nlink!=1||
       (before.st_mode&07777)!=0600||flock(held,LOCK_EX|LOCK_NB))goto fail;
    if(fstatat(base,"operations.guard",&after,AT_SYMLINK_NOFOLLOW)||!S_ISREG(after.st_mode)||after.st_nlink!=1||
       before.st_dev!=after.st_dev||before.st_ino!=after.st_ino)goto fail;
    int standalone_status=!strcmp(argv[1],"recovery-status");
    int bounded=standalone_status||!strcmp(argv[1],"recovery-admit")||!strcmp(argv[1],"recovery-retire")||!strcmp(argv[1],"recovery-backup")||!strcmp(argv[1],"recovery-install")||!strcmp(argv[1],"recovery-rollback")||!strcmp(argv[1],"recovery-start-intent")||!strcmp(argv[1],"recovery-start")||!strcmp(argv[1],"recovery-commit")||!strcmp(argv[1],"recovery-stop-current")||!strcmp(argv[1],"recovery-preserve")||!strcmp(argv[1],"recovery-retry")||!strcmp(argv[1],"recovery-commit-check");
    char *verify[]={argv[0],"recovery-code-verify",op,argv[2],migration,argv[4],NULL};
    /* Bounded entries unconditionally authenticate the complete closure in
     * recovery_context_proof before their first exec or mutation. Authenticating
     * it here as well did identical I/O twice with only native input reads in
     * between. Direct coordinator exec, including protected completion, requires this
     * verification here before executing retained code. */
    if(!bounded&&recovery_code_impl(6,verify,0))goto fail;
    if(fstatat(base,"operations.guard",&after,AT_SYMLINK_NOFOLLOW)||after.st_nlink!=1||
       before.st_dev!=after.st_dev||before.st_ino!=after.st_ino)goto fail;
    if(clearenv())goto fail;
    const char *keys[]={"PATH","LC_ALL","BRORAY_ROOT","BRORAY_STATE_ROOT","BRORAY_OPS_CODE_ROOT","BRORAY_OPS_GUARD","BRORAY_OPS_ASH","BRORAY_ROUTES_API_LOCK","BRORAY_OPS_UPDATER_ROOT","BRORAY_LEGACY_GLOBAL_LOCK","BRORAY_OPS_RAM_ROOT","BRORAY_OPS_GUARD_HELD"};
    const char *values[]={path,"C",app,state,code,guard,shell,global,updater,legacy,ram,"1"};
    for(unsigned i=0;i<sizeof keys/sizeof keys[0];i++)if(setenv(keys[i],values[i],1))goto fail;
    int flags=fcntl(held,F_GETFD);if(flags<0||fcntl(held,F_SETFD,flags&~FD_CLOEXEC)||chdir("/"))goto fail;
    if(bounded){
        /* Status acquires its own flock, then uses the exact existing read-only
         * commit proof. It never creates or terminalizes an operation. */
        char *status_args[]={argv[0],"recovery-commit-check",argv[2],argv[3],argv[4],argv[5],NULL};
        int rc=recovery_boot_admit(standalone_status?status_args:argv,held,base,op,shell,controller);
        close(held);close(base);close(root);return rc;
    }
    if(!strcmp(argv[1],"recovery-service-stop")){
        char *command[]={"ash",controller,"platform-service-stop",argv[3],argv[4],argv[5],NULL};
        execv(shell,command);goto fail;
    }
    char *command[]={shell,controller,!strcmp(argv[1],"recovery-complete")?"platform-preflight-complete":"platform-preflight-boot-context",argv[3],argv[5],NULL};
    execv(shell,command);
fail:
    if(held>=0)close(held);if(base>=0)close(base);if(root>=0)close(root);
    return migration_error("RECOVERY_ENTRY_UNCONFIRMED");
}

/* Initial boot entry delegates only to existing canonical protected phases.
 * Presence chooses a route, never grants authority: each phase independently
 * rechecks the exact closure, flock, boot boundary and durable transaction.
 * There is no retry loop, independent state writer or readiness shortcut. */
static int recovery_resume_phase(char **argv,const char *verb,int emit){
    int pipefd[2],status=0,bad=0;char output[16384];size_t used=0;
    if(pipe2(pipefd,O_CLOEXEC))return 75;pid_t parent=getpid(),child=fork();
    if(child<0){close(pipefd[0]);close(pipefd[1]);return 75;}
    if(!child){
        close(pipefd[0]);
        if(prctl(PR_SET_PDEATHSIG,SIGKILL)||getppid()!=parent||dup2(pipefd[1],STDOUT_FILENO)<0)_exit(74);
        close(pipefd[1]);
        char *command[]={argv[0],(char *)verb,argv[2],argv[3],argv[4],argv[5],NULL};
        execv("/proc/self/exe",command);_exit(74);
    }
    close(pipefd[1]);
    for(;;){char chunk[2048];ssize_t n=read(pipefd[0],chunk,sizeof chunk);
        if(n<0&&errno==EINTR)continue;if(n<0){bad=1;break;}if(!n)break;
        if((size_t)n>sizeof output-used)bad=1;else if(!bad){memcpy(output+used,chunk,(size_t)n);used+=(size_t)n;}
    }
    close(pipefd[0]);while(waitpid(child,&status,0)<0){if(errno!=EINTR){bad=1;break;}}
    if(bad||!WIFEXITED(status)||WEXITSTATUS(status)){
        if(used)fwrite(output,1,used,stdout);
        fprintf(stderr,"BRORAY_ERROR:RECOVERY_RESUME_PHASE_FAILED:%s\n",verb);return 75;
    }
    if(emit&&fwrite(output,1,used,stdout)!=used)return 75;return 0;
}
static int recovery_resume_main(int argc,char **argv){
    if(argc!=6||!migration_path(argv[2])||!token(argv[3],96)||!hex64(argv[4])||strlen(argv[5])!=32||!token(argv[5],32))return 64;
    char canonical[PATH_MAX],op[PATH_MAX];const char *prefix=strcmp(argv[2],"/")?argv[2]:"";
    if(!realpath(argv[2],canonical)||strcmp(canonical,argv[2])||
       snprintf(op,sizeof op,"%s/opt/var/lib/broray/operations/%s",prefix,argv[3])>=(int)sizeof op)return migration_error("RECOVERY_RESUME_UNCONFIRMED");
    int fd=checked_directory(op);if(fd<0)return migration_error("RECOVERY_RESUME_UNCONFIRMED");
    int installed=bg_exists(fd,"platform-install.record"),committed=bg_exists(fd,"platform-committed.record");close(fd);
    if(installed<0||committed<0)return migration_error("RECOVERY_RESUME_UNCONFIRMED");
    if(committed){
        if(service_cycle_available(argv[2])==0&&recovery_resume_phase(argv,"recovery-complete",0))return 75;
        return service_cycle_main(argc,argv,SC_START);
    }
    if(!installed&&(recovery_resume_phase(argv,"recovery-retire",0)||recovery_resume_phase(argv,"recovery-backup",0)))return 75;
    const char *phases[]={"recovery-install","recovery-start-intent","recovery-start","recovery-commit","recovery-complete"};
    for(unsigned i=0;i<sizeof phases/sizeof phases[0];i++)if(recovery_resume_phase(argv,phases[i],0))return 75;
    return service_cycle_main(argc,argv,SC_START);
}
