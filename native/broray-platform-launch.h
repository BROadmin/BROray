/* Canonical platform launch primitive. A protected transaction must separately
 * authorize/persist this exact launch record; this file never creates one and
 * never commits a transaction. Generic `run` cannot acquire platform readiness. */
struct platform_launch {
    int enabled,ready,rootfd,startfd,shellfd,nativefd,watch;
    char root[PATH_MAX],domain[PATH_MAX],id[65],manifest[65],native[65],hostsha[65];
    char shell[PATH_MAX],shellsha[65],op[97],nonce[65],startdir[PATH_MAX],launchsha[65],nativepath[PATH_MAX],host[PATH_MAX];
    struct migration_file record,transaction,files[MIGRATION_FILES];
};
static struct platform_launch pl;
static int pl_loaded;
/* Set only by the historical STOPPED bookkeeping verifier. Never grants
 * launch/control authority or watches on behalf of the historical binary. */
static int pl_historical_verification;
struct platform_watch {int wd;char child[NAME_MAX+1];};
static struct platform_watch pl_watches[256];static unsigned pl_watch_count;
static int pl_watch_one(const char *path,const char *child){
    if(pl_watch_count==256||strlen(child)>NAME_MAX)return -1;
    int wd=inotify_add_watch(pl.watch,path,IN_MODIFY|IN_ATTRIB|IN_CREATE|IN_DELETE|IN_MOVED_FROM|IN_MOVED_TO|IN_DELETE_SELF|IN_MOVE_SELF);
    if(wd<0)return -1;pl_watches[pl_watch_count].wd=wd;strcpy(pl_watches[pl_watch_count++].child,child);return 0;
}
static int pl_watch_path(const char *path){
    char prefix[PATH_MAX],child[NAME_MAX+1];if(!migration_path(path))return -1;
    strcpy(prefix,"/");const char *at=path+1;
    while(*at){const char *slash=strchr(at,'/');size_t n=slash?(size_t)(slash-at):strlen(at);if(!n||n>NAME_MAX)return -1;
        memcpy(child,at,n);child[n]=0;if(pl_watch_one(prefix,child))return -1;
        size_t used=strlen(prefix);if(used+n+2>=sizeof prefix)return -1;
        if(used>1)prefix[used++]='/';memcpy(prefix+used,at,n);prefix[used+n]=0;
        if(!slash)break;at=slash+1;
    }
    return pl_watch_one(path,"");
}
static int platform_run_check(void){
    if(!pl.enabled)return 0;union {char bytes[8192];struct inotify_event aligned;} buf;
    for(;;){ssize_t n=read(pl.watch,buf.bytes,sizeof buf.bytes);if(n<0&&errno==EINTR)continue;if(n<0&&errno==EAGAIN)return 0;if(n<=0)return -1;
        for(size_t at=0;at<(size_t)n;){struct inotify_event *e=(void*)(buf.bytes+at);at+=sizeof *e+e->len;
            if(e->mask&IN_Q_OVERFLOW)return -1;
            for(unsigned i=0;i<pl_watch_count;i++)if(e->wd==pl_watches[i].wd&&
                (!e->len||!pl_watches[i].child[0]||!strcmp(e->name,pl_watches[i].child)))return -1;
        }
    }
}
static int pl_exact(void){
    if(bg_record_exact(pl.startfd,"launch.record",pl.record.bytes,pl.record.size))return -1;
    if(pl.transaction.present&&bg_record_exact(pl.startfd,"transaction.record",pl.transaction.bytes,pl.transaction.size))return -1;
    for(int i=0;i<MIGRATION_FILES;i++)if(bg_exact(pl.rootfd,migration_paths[i],pl.files[i].bytes,pl.files[i].size,0755))return -1;
    return service_interpreter_valid(pl.shellfd,pl.shell,pl.shellsha);
}
static int platform_request_check(void){return pl.enabled&&(platform_run_check()||pl_exact())?-1:0;}
static int pl_load(const char *directory,const char *expected,const char *transaction,int watch){
    if(pl_historical_verification&&watch)return -1;
    if(pl_loaded){
        free(pl.record.bytes);free(pl.transaction.bytes);for(int i=0;i<MIGRATION_FILES;i++)free(pl.files[i].bytes);
        int fds[]={pl.rootfd,pl.startfd,pl.shellfd,pl.nativefd,pl.watch};
        for(unsigned i=0;i<sizeof fds/sizeof fds[0];i++)if(fds[i]>=0)close(fds[i]);
    }
    memset(&pl,0,sizeof pl);pl.rootfd=pl.startfd=pl.shellfd=pl.nativefd=pl.watch=-1;
    pl_loaded=1;pl_watch_count=0;
    if(!migration_path(directory)||!hex64(expected))return -1;
    strcpy(pl.startdir,directory);strcpy(pl.launchsha,expected);pl.startfd=checked_directory(directory);
    if(pl.startfd<0||migration_read(pl.startfd,"launch.record",&pl.record,0)||pl.record.mode!=0600||strcmp(pl.record.sha,expected)||memchr(pl.record.bytes,0,pl.record.size))return -1;
    const char magic[]="BROray-platform-launch/1\n",protected_magic[]="BROray-platform-launch/2\n";
    int exact_manifest=pl.record.size>=sizeof protected_magic-1&&!memcmp(pl.record.bytes,protected_magic,sizeof protected_magic-1);
    if(pl.record.size<sizeof magic-1||(!exact_manifest&&memcmp(pl.record.bytes,magic,sizeof magic-1)))return -1;
    /* Only the protected controller assigns meaning to the transaction bytes.
     * This lifetime boundary binds its independently expected digest, rejects
     * missing/unknown evidence, and never reconstructs a transaction record. */
    if(transaction&&(!exact_manifest||!hex64(transaction)||migration_read(pl.startfd,"transaction.record",&pl.transaction,0)||
       pl.transaction.mode!=0600||strcmp(pl.transaction.sha,transaction)))return -1;
    char *dest[]={pl.root,pl.domain,pl.id,pl.manifest,pl.native,pl.shell,pl.shellsha,pl.op,pl.nonce};
    size_t limits[]={sizeof pl.root,sizeof pl.domain,sizeof pl.id,sizeof pl.manifest,sizeof pl.native,sizeof pl.shell,sizeof pl.shellsha,sizeof pl.op,sizeof pl.nonce};
    size_t at=sizeof magic-1;
    for(unsigned i=0;i<9;i++){char *end=memchr(pl.record.bytes+at,'\n',pl.record.size-at);if(!end)return -1;size_t n=(size_t)(end-pl.record.bytes)-at;
        if(!n||n>=limits[i])return -1;memcpy(dest[i],pl.record.bytes+at,n);dest[i][n]=0;at+=n+1;}
    if((!exact_manifest&&at!=pl.record.size)||!migration_path(pl.root)||!migration_path(pl.domain)||!migration_path(pl.shell)||!token(pl.id,64)||!hex64(pl.manifest)||!hex64(pl.native)||!hex64(pl.shellsha)||!token(pl.op,96)||!token(pl.nonce,64))return -1;
    const char *prefix=strcmp(pl.root,"/")?pl.root:"";char want[PATH_MAX],canonical[PATH_MAX];
    if(!realpath(pl.root,canonical)||strcmp(canonical,pl.root)||!realpath(directory,canonical)||strcmp(canonical,directory))return -1;
    if(snprintf(want,sizeof want,"%s/opt/var/lib/broray-updater/generations/%s",prefix,pl.id)>=(int)sizeof want||strcmp(want,pl.domain))return -1;
    if(snprintf(want,sizeof want,"%s/opt/var/lib/broray-updater/starts/%s",prefix,pl.id)>=(int)sizeof want||strcmp(want,directory))return -1;
    if(snprintf(pl.host,sizeof pl.host,"%s/opt/var/lib/broray-updater/hosts/%s",prefix,pl.id)>=(int)sizeof pl.host||strlen(pl.host)+sizeof "/control">sizeof(((struct sockaddr_un *)0)->sun_path))return -1;
    pl.rootfd=migration_directory(pl.root);if(pl.rootfd<0)return -1;
    struct gen_sha manifest_hash;gen_sha_init(&manifest_hash);
    for(int i=0;i<MIGRATION_FILES;i++){
        if(migration_read(pl.rootfd,migration_paths[i],&pl.files[i],0)||pl.files[i].mode!=0755)return -1;
        gen_sha_add(&manifest_hash,pl.files[i].sha,64);gen_sha_add(&manifest_hash,"  ",2);gen_sha_add(&manifest_hash,migration_paths[i],strlen(migration_paths[i]));gen_sha_add(&manifest_hash,"\n",1);
    }
    char sha[65];gen_sha_end(&manifest_hash,sha);
    if(exact_manifest){
        /* Retain the authenticated manifest serialization: the migration
         * contract permits any line order, but never a duplicate/extra path.
         * The immutable launch record binds and watches these exact bytes. */
        digest_bytes(pl.record.bytes+at,pl.record.size-at,sha);
        unsigned seen=0;
        while(at<pl.record.size){
            char *line=pl.record.bytes+at,*nl=memchr(line,'\n',pl.record.size-at);if(!nl)return -1;
            int matched=0;
            for(int i=0;i<MIGRATION_FILES;i++){char row[256];int n=snprintf(row,sizeof row,"%s  %s",pl.files[i].sha,migration_paths[i]);
                if(n==(int)(nl-line)&&!memcmp(line,row,(size_t)n)&&!(seen&(1U<<i))){seen|=1U<<i;matched=1;break;}}
            if(!matched)return -1;at=(size_t)(nl-pl.record.bytes)+1;
        }
        if(seen!=((1U<<MIGRATION_FILES)-1))return -1;
    }
    if(strcmp(sha,pl.manifest))return -1;
    if(pl_historical_verification){
        int n=snprintf(pl.nativepath,sizeof pl.nativepath,"%s/opt/var/lib/broray-updater/runtimes/%s/runtime",prefix,pl.native);
        if(n<0||n>=(int)sizeof pl.nativepath)return -1;
        int parent=migration_directory("/");if(parent<0)return -1;
        pl.nativefd=migration_relative(parent,pl.nativepath+1);close(parent);struct stat st;
        if(pl.nativefd<0||fstat(pl.nativefd,&st)||!S_ISREG(st.st_mode)||st.st_uid!=geteuid()||st.st_nlink!=1||
           (st.st_mode&07777)!=0700||hash_fd(pl.nativefd,sha)||strcmp(sha,pl.native))return -1;
    }else{
        pl.nativefd=open("/proc/self/exe",O_RDONLY|O_CLOEXEC);if(pl.nativefd<0||hash_fd(pl.nativefd,sha)||strcmp(sha,pl.native))return -1;
        ssize_t named=readlink("/proc/self/exe",pl.nativepath,sizeof pl.nativepath-1);if(named<=0||named>=(ssize_t)sizeof pl.nativepath-1)return -1;pl.nativepath[named]=0;
    }
    int fs=migration_directory("/");if(fs<0)return -1;pl.shellfd=migration_relative(fs,pl.shell+1);close(fs);
    if(pl.shellfd<0||pl_exact())return -1;
    /* Verification fsyncs the existing immutable intent; it never reconstructs
     * a missing record or overwrites unknown bytes. */
    if(migration_record(pl.startfd,"launch.record",pl.record.bytes,pl.record.size,0))return -1;
    if(pl.transaction.present&&migration_record(pl.startfd,"transaction.record",pl.transaction.bytes,pl.transaction.size,0))return -1;
    if(watch){
        pl.watch=inotify_init1(IN_NONBLOCK|IN_CLOEXEC);if(pl.watch<0)return -1;
        if(snprintf(want,sizeof want,"%s/launch.record",directory)>=(int)sizeof want||pl_watch_path(want)||pl_watch_path(pl.shell)||pl_watch_path(pl.nativepath))return -1;
        if(pl.transaction.present&&(snprintf(want,sizeof want,"%s/transaction.record",directory)>=(int)sizeof want||pl_watch_path(want)))return -1;
        for(int i=0;i<MIGRATION_FILES;i++)if(snprintf(want,sizeof want,"%s/%s",prefix,migration_paths[i])>=(int)sizeof want||pl_watch_path(want))return -1;
    }
    pl.enabled=1;return pl_exact()||(watch&&platform_run_check())?-1:0;
}
static int platform_host_live(void);
static int platform_run_prepare(int argc,char **argv){
    if(argc<8||strcmp(argv[7],"platform-daemon"))return 0;
    if((argc!=10&&argc!=11&&argc!=12)||pl_load(argv[8],argv[9],argc>=11?argv[10]:NULL,1)||strcmp(argv[2],pl.domain)||strcmp(argv[3],pl.id)||strcmp(argv[4],pl.manifest))return -1;
    if(argc==12){if(!hex64(argv[11]))return -1;strcpy(pl.hostsha,argv[11]);if(platform_host_live())return -1;}
    char actual[PATH_MAX];if(!realpath(argv[6],actual)||strcmp(actual,pl.nativepath))return -1;return 0;
}
static void platform_exec_root(char **args){
    extern char **environ;if(pl.enabled)fexecve(pl.nativefd,args,environ);else execv(args[0],args);
}
static void platform_state_json(FILE *f,const char *next){
    int ready=pl.enabled&&pl.ready&&root_live&&!stopping&&!strcmp(next,"RUNNING");
    fprintf(f,",\"platformReady\":%s,\"platformLaunch\":",ready?"true":"false");
    if(!pl.enabled){fputs("null",f);return;}
    fprintf(f,"{\"contract\":\"broray-platform-launch/1\",\"startIntentSha256\":\"%s\",\"daemonSha256\":\"%s\",\"nativeSha256\":\"%s\",\"interpreterSha256\":\"%s\",\"operationId\":\"%s\",\"stopNonce\":\"%s\",\"transactionRecordSha256\":",pl.launchsha,pl.files[5].sha,pl.native,pl.shellsha,pl.op,pl.nonce);
    if(pl.transaction.present)json_string(f,pl.transaction.sha);else fputs("null",f);fputc('}',f);
    fputs(",\"serviceHostRecordSha256\":",f);if(pl.hostsha[0])json_string(f,pl.hostsha);else fputs("null",f);
}
static int pl_private_directory(const char *relative){
    int fd=migration_relative(pl.rootfd,relative);if(fd<0)return -1;struct stat held,named;
    if(fstat(fd,&held)||!S_ISDIR(held.st_mode)||held.st_uid!=geteuid()||(held.st_mode&07777)!=0700||
       fstatat(pl.rootfd,relative,&named,AT_SYMLINK_NOFOLLOW)||held.st_dev!=named.st_dev||held.st_ino!=named.st_ino||held.st_mode!=named.st_mode||held.st_uid!=named.st_uid){close(fd);return -1;}
    return fd;
}
static int pl_queue_ready(void){
    int queue=pl_private_directory("opt/var/lib/broray-updater/queue");if(queue<0)return -1;
    int bad=empty_directory(queue)!=1;close(queue);if(bad||bg_exists(pl.rootfd,"opt/var/lib/broray-updater/request.lock")!=0)return -1;
    char expected[32];int n=snprintf(expected,sizeof expected,"%d\n",updater.pid);
    if(bg_exact(pl.rootfd,"opt/var/lib/broray-updater/daemon.pid",expected,(size_t)n,0600)||bg_exact(pl.rootfd,"opt/var/lib/broray-updater/daemon.ready",expected,(size_t)n,0600))return -1;
    int fd=pl_private_directory("opt/var/lib/broray-updater/daemon.lock");if(fd<0)return -1;
    bad=empty_directory(fd)!=1;close(fd);return bad?-1:0;
}
static int platform_control(const struct ucred *peer,const char *verb,const char *op,const char *nonce){
    if(!pl.enabled||!root_live||stopping||strcmp(state,"RUNNING")||strcmp(op,pl.op)||strcmp(nonce,pl.nonce))return -1;
    int i=kid_index(peer->pid);struct identity now;if(i<0||awaiting_birth[i]||capture(peer->pid,&now)||!identity_equal(&now,&kids[i]))return -1;
    char sha[65];if(peer_executable_hash(peer->pid,sha)||strcmp(sha,pl.native))return -1;
    if(platform_request_check())return -2;
    if(!strcmp(verb,"AUTH"))return 0;
    if(strcmp(verb,"READY")||capture(updater.pid,&now)||!identity_equal(&now,&updater)||strcmp(updater.exe,pl.shell)||pl_queue_ready())return -1;
    if(pl.ready)return 0;pl.ready=1;return 1;
}
static int platform_host_identity(struct identity *owner){
    char *args[]={pl.nativepath,"service-status",pl.host,pl.domain,pl.id,pl.manifest,pl.root,pl.shell,pl.shellsha,NULL};
    char sha[65];return service_host_probe(args,owner)||
        (pl.hostsha[0]&&(service_host_record_sha(args,owner,sha)||strcmp(sha,pl.hostsha)))?-1:0;
}
static int platform_host_live(void){struct identity owner;return platform_host_identity(&owner);}
static int platform_daemon(int argc,char **argv){
    if((argc!=4&&argc!=5&&argc!=6)||pl_load(argv[2],argv[3],argc>=5?argv[4]:NULL,0))return service_error("PLATFORM_LAUNCH_UNCONFIRMED");
    if(argc==6){if(!hex64(argv[5]))return service_error("PLATFORM_HOST_PIN_UNCONFIRMED");strcpy(pl.hostsha,argv[5]);}
    /* Environment carries no authority. An exact live supervisor must prove
     * this process is one of its registered descendants before script exec. */
    char *auth[]={argv[0],"control",pl.domain,"AUTH",pl.id,pl.manifest,pl.op,pl.nonce};
    if(control_exchange(8,auth,0))return service_error("PLATFORM_ORIGIN_UNCONFIRMED");
    if(pl.transaction.present&&platform_host_live())return service_error("PLATFORM_SERVICE_HOST_UNCONFIRMED");
    const char *readenv=getenv("BRORAY_UPDATER_IDLE_READ_FD"),*writeenv=getenv("BRORAY_UPDATER_IDLE_WRITE_FD");
    char *end;long rd=readenv?strtol(readenv,&end,10):-1;if(!readenv||*end||rd<3||rd>INT_MAX)return 75;
    long wr=writeenv?strtol(writeenv,&end,10):-1;if(!writeenv||*end||wr<3||wr>INT_MAX||wr==rd)return 75;
    struct stat a,b;if(fstat((int)rd,&a)||fstat((int)wr,&b)||!S_ISFIFO(a.st_mode)||a.st_dev!=b.st_dev||a.st_ino!=b.st_ino||(fcntl((int)rd,F_GETFL)&O_ACCMODE)!=O_RDONLY||(fcntl((int)wr,F_GETFL)&O_ACCMODE)!=O_WRONLY)return 75;
    int p[2];if(pipe2(p,O_CLOEXEC))return 74;pid_t feeder=fork();if(feeder<0)return 74;
    if(!feeder){close(p[0]);service_close_fds(p[1],-1);size_t at=0;while(at<pl.files[5].size){ssize_t n=write(p[1],pl.files[5].bytes+at,pl.files[5].size-at);if(n<0&&errno==EINTR)continue;if(n<=0)_exit(74);at+=(size_t)n;}close(p[1]);_exit(0);}
    close(p[1]);char readfd[24],writefd[24],script[64],updater_root[PATH_MAX];snprintf(readfd,sizeof readfd,"%ld",rd);snprintf(writefd,sizeof writefd,"%ld",wr);snprintf(script,sizeof script,"/proc/self/fd/%d",p[0]);
    const char *prefix=strcmp(pl.root,"/")?pl.root:"";if(snprintf(updater_root,sizeof updater_root,"%s/opt/var/lib/broray-updater",prefix)>=(int)sizeof updater_root)return 74;
    if(clearenv())return 74;
    const char *keys[]={"PATH","LC_ALL","BRORAY_UPDATER_GENERATION_NATIVE","BRORAY_UPDATER_GENERATION_ROOT","BRORAY_UPDATER_GENERATION","BRORAY_UPDATER_MANIFEST_SHA256","BRORAY_UPDATER_OPERATION_ID","BRORAY_UPDATER_STOP_NONCE","BRORAY_UPDATER_STATE_ROOT","BRORAY_UPDATER_ROOT_PREFIX","BRORAY_UPDATER_IDLE_READ_FD","BRORAY_UPDATER_IDLE_WRITE_FD","BRORAY_UPDATER_DAEMON_SHA256","BRORAY_UPDATER_SERVICE_HOST","BRORAY_UPDATER_SERVICE_INTERPRETER","BRORAY_UPDATER_SERVICE_INTERPRETER_SHA256"};
    const char *values[]={"/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin","C",pl.nativepath,pl.domain,pl.id,pl.manifest,pl.op,pl.nonce,updater_root,prefix,readfd,writefd,pl.files[5].sha,pl.host,pl.shell,pl.shellsha};
    for(unsigned i=0;i<sizeof keys/sizeof keys[0];i++)if(setenv(keys[i],values[i],1))return 74;
    if(fcntl(p[0],F_SETFD,0)<0)return 74;
    DIR *d=opendir("/proc/self/fd");if(!d)return 74;int own=dirfd(d);struct dirent *e;
    while((e=readdir(d))){char *tail;long fd=strtol(e->d_name,&tail,10);if(*tail||fd<3||fd==own||fd==p[0]||fd==pl.shellfd||fd==rd||fd==wr)continue;close((int)fd);}closedir(d);
    char *args[]={"ash",script,"daemon",NULL};extern char **environ;fexecve(pl.shellfd,args,environ);return 74;
}

/* START_INTENT is durable preparation only. The existing guarded native
 * executor is the sole writer here; no process is launched or signalled. */
static void platform_generation_id(const char seed[65],char id[65]){
    /* Compact directory component for sockaddr_un, not an authorization
     * token. The full 256-bit seed and both record digests remain mandatory;
     * a namespace collision fails exact-record validation without adoption. */
    static const char alphabet[]="ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_";
    unsigned bits=0,value=0;size_t out=2;id[0]='g';id[1]='-';
    for(int i=0;i<32;i++){
        unsigned nibble=seed[i]<='9'?(unsigned)(seed[i]-'0'):(unsigned)(seed[i]-'a'+10);
        value=(value<<4)|nibble;bits+=4;
        if(bits>=6){bits-=6;id[out++]=alphabet[(value>>bits)&63];}
    }
    if(bits)id[out++]=alphabet[(value<<(6-bits))&63];id[out]=0;
}
static struct {char updater[PATH_MAX],start[PATH_MAX],domain[PATH_MAX],runtime[PATH_MAX],id[65],launch[65],transaction[65];} ps;
/* Attempt records extend only protected launch evidence. The original
 * migration/install context and installation lifetime mutex never move. */
#define PLATFORM_ATTEMPT_LIMIT 32
static struct {
    int root,parent,count,loaded;
    struct {int fd;char id[65],seed[65];} node[PLATFORM_ATTEMPT_LIMIT];
} pa={.root=-1,.parent=-1};
static int psc_pair(int op,const char *name,const char *anchor);
static int platform_attempt_index(int op){
    if(!pa.loaded)return 0;struct stat st,want;if(fstat(op,&st))return -1;
    for(int i=0;i<=pa.count;i++)if(!fstat(pa.node[i].fd,&want)&&st.st_dev==want.st_dev&&st.st_ino==want.st_ino)return i;
    return -1;
}
static int platform_context_op(int op){return !pa.loaded?op:platform_attempt_index(op)>=0?pa.root:-1;}
static int platform_attempt_current(int op){return !pa.loaded?op:platform_attempt_index(op)>=0?pa.node[pa.count].fd:-1;}
static int platform_attempt_required(int op,int required){return platform_attempt_index(op)==pa.count?required:-1;}
static int platform_start_seed(char out[65],char id[65],const char *root,const struct bg_input *input,const char *migration,const char *installed,const char *native,const char *shell,const char *shell_sha){
    char seed[PATH_MAX*2+1024];int n=snprintf(seed,sizeof seed,"BROray-platform-start-generation/1\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n",root,input->operation,input->nonce,boot,migration,installed,input->manifest,native,shell,shell_sha);
    if(n<0||n>=(int)sizeof seed)return -1;digest_bytes(seed,(size_t)n,out);platform_generation_id(out,id);return 0;
}
static int platform_retry_text(char record[2048],char seed_sha[65],char id[65],const struct bg_input *input,const char *migration,const char *native,const char *installed,const char *previous_id,const char *previous_launch,const char *previous_transaction,const char *stopped,const char *retained){
    char seed[1536];int sn=snprintf(seed,sizeof seed,"BROray-platform-retry-generation/1\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n",input->operation,input->nonce,boot,migration,input->manifest,native,installed,previous_id,previous_transaction,stopped,retained);
    if(sn<0||sn>=(int)sizeof seed)return -1;digest_bytes(seed,(size_t)sn,seed_sha);platform_generation_id(seed_sha,id);
    if(!strcmp(id,previous_id))return -1;
    return snprintf(record,2048,"BROray-platform-retry/1\nRETRY_PREPARED\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n",input->operation,input->nonce,boot,migration,input->manifest,native,installed,previous_id,previous_launch,previous_transaction,stopped,retained,seed_sha,id);
}
/* Namespace membership is derived from the whole verified chain. The leaf
 * may be absent only during preparation; every predecessor remains present. */
static int platform_attempt_names(int fd,int generation,int leaf_required,const char *fallback){
    if(!pa.loaded||!pa.count){
        if(!leaf_required)return bg_empty(fd);
        const char *names[]={fallback,".generation-lifetime.lock"};return rc_names(fd,names,generation?2:1);
    }
    DIR *d=directory_stream(fd);if(!d)return -1;struct dirent *e;unsigned char seen[PLATFORM_ATTEMPT_LIMIT]={0};int lock=0,bad=0;errno=0;
    while((e=readdir(d))){const char *name=e->d_name;if(!strcmp(name,".")||!strcmp(name,".."))continue;
        if(generation&&!strcmp(name,".generation-lifetime.lock")){if(lock++){bad=1;break;}continue;}
        int at=-1;for(int i=0;i<=pa.count;i++)if(!strcmp(name,pa.node[i].id))at=i;
        struct stat st;if(at<0||seen[at]++||fstatat(fd,name,&st,AT_SYMLINK_NOFOLLOW)||!S_ISDIR(st.st_mode)||st.st_uid!=geteuid()||(st.st_mode&07777)!=0700){bad=1;break;}errno=0;
    }
    if(!e&&errno)bad=1;closedir(d);
    for(int i=0;i<pa.count;i++)if(!seen[i])bad=1;
    if(leaf_required>0&&!seen[pa.count])bad=1;
    if(!leaf_required&&seen[pa.count])bad=1;
    return bad||(generation&&!lock)?-1:0;
}
static int platform_attempt_files(int fd){
    const char *names[]={"attempt.record","platform-start.record","platform-starting.record","platform-host-starting.record","platform-host-owner.record","platform-host-owner.anchor","platform-stop-current.intent","platform-stop-current.intent-anchor","platform-stop-current.receipt","platform-stop-current.receipt-anchor","platform-committed.record","platform-committed.anchor","platform-retained.record","platform-retained.anchor","platform-retry.record","platform-retry.anchor"};
    DIR *d=directory_stream(fd);if(!d)return -1;struct dirent *e;int bad=0;errno=0;
    while((e=readdir(d))){const char *name=e->d_name;if(!strcmp(name,".")||!strcmp(name,".."))continue;int allowed=0;
        for(unsigned i=0;i<sizeof names/sizeof names[0];i++)if(!strcmp(name,names[i]))allowed=1;
        struct stat st;if(!allowed||fstatat(fd,name,&st,AT_SYMLINK_NOFOLLOW)||!S_ISREG(st.st_mode)||st.st_uid!=geteuid()||st.st_nlink!=1||(st.st_mode&07777)!=0600){bad=1;break;}errno=0;
    }
    if(!e&&errno)bad=1;closedir(d);return bad?-1:0;
}
static int platform_attempt_binding(int fd,char launch[65],char transaction[65]){
    struct migration_file r;memset(&r,0,sizeof r);char extra,text[256];int result=-1;
    if(migration_read(fd,"platform-start.record",&r,0)||r.mode!=0600||memchr(r.bytes,0,r.size)||
       sscanf(r.bytes,"BROray-platform-start-binding/1\n%64[0-9a-f]\n%64[0-9a-f]\n%c",transaction,launch,&extra)!=2||!hex64(transaction)||!hex64(launch))goto done;
    int n=snprintf(text,sizeof text,"BROray-platform-start-binding/1\n%s\n%s\n",transaction,launch);
    if(n<0||n>=(int)sizeof text||r.size!=(size_t)n||memcmp(r.bytes,text,(size_t)n))goto done;result=0;
done:free(r.bytes);return result;
}
/* Read-only chain reconstruction; all candidate digests are checked against
 * complete predecessor STOPPED/host proofs before returning any selected fd. */
static int platform_attempt_load(int op,const char *op_path,const char *root,const struct bg_input *input,const char *migration,const char *native){
    int result=-1,fs=-1,ash=-1;struct migration_file installed;memset(&installed,0,sizeof installed);
    char parent_path[PATH_MAX],path[PATH_MAX],shell_path[PATH_MAX],shell[PATH_MAX],shell_sha[65];
    if(!pa.loaded){
        for(int i=0;i<PLATFORM_ATTEMPT_LIMIT;i++)pa.node[i].fd=-1;
        pa.root=fcntl(op,F_DUPFD_CLOEXEC,3);if(pa.root<0)return -1;pa.node[0].fd=pa.root;pa.loaded=1;
    }else if(platform_context_op(op)<0)return -1;
    int first=psc_pair(pa.root,"platform-retry.record","platform-retry.anchor");
    if(first<0)return -1;
    if(!first)return pa.count||bg_exists(pa.root,"platform-attempts")!=0?-1:0;
    const char *prefix=strcmp(root,"/")?root:"";
    if(migration_read(pa.root,"platform-install/installed.receipt",&installed,0)||installed.mode!=0600||
       snprintf(parent_path,sizeof parent_path,"%s/platform-attempts",op_path)>=(int)sizeof parent_path||
       snprintf(shell_path,sizeof shell_path,"%s/opt/bin/ash",prefix)>=(int)sizeof shell_path||!realpath(shell_path,shell))goto done;
    fs=migration_directory("/");if(fs<0)goto done;ash=migration_relative(fs,shell+1);
    if(ash<0||hash_fd(ash,shell_sha)||service_interpreter_valid(ash,shell,shell_sha)||
       platform_start_seed(pa.node[0].seed,pa.node[0].id,root,input,migration,installed.sha,native,shell,shell_sha))goto done;
    if(pa.parent<0)pa.parent=checked_directory(parent_path);if(pa.parent<0)goto done;
    struct stat named,held;if(fstat(pa.parent,&held)||fstatat(pa.root,"platform-attempts",&named,AT_SYMLINK_NOFOLLOW)||held.st_dev!=named.st_dev||held.st_ino!=named.st_ino||!S_ISDIR(named.st_mode)||(named.st_mode&07777)!=0700||named.st_uid!=geteuid())goto done;
    int count=0;
    for(;;){
        int parent=pa.node[count].fd,present=psc_pair(parent,"platform-retry.record","platform-retry.anchor");if(present<0)goto done;
        if(!present)break;if(count+1>=PLATFORM_ATTEMPT_LIMIT)goto done;
        struct migration_file stopped,retained;memset(&stopped,0,sizeof stopped);memset(&retained,0,sizeof retained);
        char launch[65],transaction[65],id[65],seed[65],record[2048],anchor[128],sha[65];int bad=1;
        if(!platform_attempt_binding(parent,launch,transaction)&&!migration_read(parent,"platform-stop-current.receipt",&stopped,0)&&stopped.mode==0600&&
           !migration_read(parent,"platform-retained.record",&retained,0)&&retained.mode==0600){
            int n=platform_retry_text(record,seed,id,input,migration,native,installed.sha,pa.node[count].id,launch,transaction,stopped.sha,retained.sha);
            if(n>0&&n<(int)sizeof record){
                digest_bytes(record,(size_t)n,sha);int an=snprintf(anchor,sizeof anchor,"BROray-platform-retry-anchor/1\n%s\n",sha);
                if(an>0&&an<(int)sizeof anchor&&!bg_record_exact(parent,"platform-retry.record",record,(size_t)n)&&!bg_record_exact(parent,"platform-retry.anchor",anchor,(size_t)an)&&
                   snprintf(path,sizeof path,"%s/%s",parent_path,id)<(int)sizeof path){
                    int unique=1;for(int j=0;j<=count;j++)if(!strcmp(id,pa.node[j].id))unique=0;
                    if(unique){
                        int next=count+1;if(pa.node[next].fd<0)pa.node[next].fd=checked_directory(path);
                        if(pa.node[next].fd>=0&&!fstat(pa.node[next].fd,&held)&&!fstatat(pa.parent,id,&named,AT_SYMLINK_NOFOLLOW)&&held.st_dev==named.st_dev&&held.st_ino==named.st_ino&&
                           S_ISDIR(named.st_mode)&&(named.st_mode&07777)==0700&&named.st_uid==geteuid()&&!platform_attempt_files(pa.node[next].fd)&&
                           !bg_record_exact(pa.node[next].fd,"attempt.record",record,(size_t)n)){
                            if(next>pa.count||(!strcmp(pa.node[next].id,id)&&!strcmp(pa.node[next].seed,seed))){strcpy(pa.node[next].id,id);strcpy(pa.node[next].seed,seed);bad=0;}
                        }
                    }
                }
            }
        }
        free(stopped.bytes);free(retained.bytes);if(bad)goto done;count++;
    }
    if(count<pa.count)goto done;pa.count=count;
    /* Operation attempt directories omit the original root attempt. */
    DIR *dir=directory_stream(pa.parent);if(!dir)goto done;struct dirent *e;unsigned found=0;int bad=0;errno=0;
    while((e=readdir(dir))){if(!strcmp(e->d_name,".")||!strcmp(e->d_name,".."))continue;int known=0;
        for(int i=1;i<=pa.count;i++)if(!strcmp(e->d_name,pa.node[i].id))known=1;
        if(!known){bad=1;break;}found++;errno=0;
    }
    if(!e&&errno)bad=1;closedir(dir);if(bad||found!=(unsigned)pa.count)goto done;
    for(int i=0;i<pa.count;i++)if(platform_retained_proof(pa.node[i].fd,input,migration,native)||
       platform_stopped_current_proof(pa.node[i].fd,op_path,root,input,migration,native))goto done;
    result=0;
done:free(installed.bytes);if(ash>=0)close(ash);if(fs>=0)close(fs);return result;
}
static int platform_attempt_prestart(int op,const char *op_path,const char *root,const struct bg_input *input,const char *migration,const char *native){
    int index=platform_attempt_index(op);if(index<=0||index!=pa.count)return -1;
    const char *absent[]={"platform-starting.record","platform-host-starting.record","platform-host-owner.record","platform-host-owner.anchor","platform-stop-current.intent","platform-stop-current.intent-anchor","platform-stop-current.receipt","platform-stop-current.receipt-anchor","platform-committed.record","platform-committed.anchor"};
    for(unsigned i=0;i<sizeof absent/sizeof absent[0];i++)if(bg_exists(op,absent[i])!=0)return -1;
    char path[PATH_MAX];const char *prefix=strcmp(root,"/")?root:"";
    for(int family=0;family<2;family++){
        if(snprintf(path,sizeof path,"%s/opt/var/lib/broray-updater/%s",prefix,family?"hosts":"generations")>=(int)sizeof path)return -1;
        int fd=checked_directory(path);if(fd<0)return -1;int bad=platform_attempt_names(fd,!family,0,NULL);close(fd);if(bad)return -1;
    }
    int started=bg_exists(op,"platform-start.record");if(started<0)return -1;
    if(started){
        char shell[PATH_MAX];if(snprintf(shell,sizeof shell,"%s/opt/bin/ash",prefix)>=(int)sizeof shell)return -1;
        char *args[]={"retry-prestart","recovery-start",(char*)root,(char*)input->operation,(char*)migration,(char*)input->nonce};
        if(platform_start_intent_apply(args,op,op_path,input,native,NULL,-1,-1,shell,NULL,1))return -1;
    }
    return 0;
}
static int platform_attempt_control_view(int op,const char *op_path,const char *root,const struct bg_input *input,const char *migration,const char *native){
    if(platform_attempt_load(op,op_path,root,input,migration,native))return -1;
    op=platform_attempt_current(op);if(op<0)return -1;
    if(!platform_attempt_prestart(op,op_path,root,input,migration,native))return 0;
    int stopped=psc_pair(op,"platform-stop-current.receipt","platform-stop-current.receipt-anchor");
    if(stopped<0)return -1;
    return stopped?platform_stopped_current_proof(op,op_path,root,input,migration,native):platform_stop_current_proof(op,op_path,root,input,migration,native);
}


static int platform_witness_directory(int startfd,const char *start,int required){
    int present=bg_exists(startfd,"ledger-witnesses");if(present<0||(!present&&required))return -1;
    if(!present)return 0;
    char path[PATH_MAX];if(snprintf(path,sizeof path,"%s/ledger-witnesses",start)>=(int)sizeof path)return -1;
    int fd=checked_directory(path);if(fd<0)return -1;struct stat held,named;int bad=0;
    if(fstat(fd,&held)||fstatat(startfd,"ledger-witnesses",&named,AT_SYMLINK_NOFOLLOW)||
       !S_ISDIR(named.st_mode)||named.st_uid!=geteuid()||(named.st_mode&07777)!=0700||
       held.st_dev!=named.st_dev||held.st_ino!=named.st_ino||held.st_mode!=named.st_mode||held.st_uid!=named.st_uid)bad=1;
    if(!bad&&required){
        struct stat state;if(fstatat(fd,"state.json",&state,AT_SYMLINK_NOFOLLOW)||!S_ISREG(state.st_mode)||
           state.st_uid!=geteuid()||state.st_nlink!=1||(state.st_mode&07777)!=0600)bad=1;
    }
    close(fd);return bad?-1:0;
}

static int platform_start_intent_apply(char **argv,int op,const char *op_path,const struct bg_input *input,const char *native,const struct identity *executor,int held,int statefd,const char *shell,const char *controller,int observe){
    int updaterfd=-1,runtimefd=-1,startsfd=-1,startfd=-1,shellfd=-1,fs=-1,result=75;
    struct migration_file installed,manifest;memset(&installed,0,sizeof installed);memset(&manifest,0,sizeof manifest);
    char updater_path[PATH_MAX],runtime_dir[PATH_MAX],runtime_path[PATH_MAX],starts[PATH_MAX],start[PATH_MAX],domain[PATH_MAX],resolved[PATH_MAX];
    char shell_sha[65],runtime_record[256],seed_sha[65],id[65],launch_sha[65],transaction_sha[65],binding[256];
    char *launch=NULL,*transaction=NULL;size_t launch_size=0,transaction_size=0;
    const char *prefix=strcmp(argv[2],"/")?argv[2]:"";
    if((!observe&&platform_install_apply(argv,platform_context_op(op),op_path,input,native,executor,held,statefd,shell,controller,1))||
       migration_read(platform_context_op(op),"platform-install/installed.receipt",&installed,0)||installed.mode!=0600||
       migration_read(platform_context_op(op),"platform-migration/manifest.record",&manifest,0)||manifest.mode!=0600||strcmp(manifest.sha,input->manifest)||
       snprintf(updater_path,sizeof updater_path,"%s/opt/var/lib/broray-updater",prefix)>=(int)sizeof updater_path||
       snprintf(runtime_dir,sizeof runtime_dir,"%s/runtimes/%s",updater_path,native)>=(int)sizeof runtime_dir||
       snprintf(runtime_path,sizeof runtime_path,"%s/runtime",runtime_dir)>=(int)sizeof runtime_path||
       snprintf(starts,sizeof starts,"%s/starts",updater_path)>=(int)sizeof starts||
       !realpath(shell,resolved)||!migration_path(resolved))goto done;
    updaterfd=checked_directory(updater_path);runtimefd=checked_directory(runtime_dir);fs=migration_directory("/");
    if(updaterfd<0||runtimefd<0||fs<0||runtime_names(runtimefd)||bg_runtime_valid(runtimefd,native))goto done;
    int rn=snprintf(runtime_record,sizeof runtime_record,"{\"schemaVersion\":1,\"contract\":\"broray-updater-runtime/1\",\"runtimeSha256\":\"%s\",\"mode\":448,\"processAuthority\":false}\n",native);
    if(rn<0||rn>=(int)sizeof runtime_record||bg_record_exact(runtimefd,"identity.json",runtime_record,(size_t)rn))goto done;
    shellfd=migration_relative(fs,resolved+1);
    if(shellfd<0||hash_fd(shellfd,shell_sha)||service_interpreter_valid(shellfd,resolved,shell_sha))goto done;
    int attempt_index=platform_attempt_index(op);if(attempt_index<0)goto done;
    if(attempt_index){strcpy(seed_sha,pa.node[attempt_index].seed);strcpy(id,pa.node[attempt_index].id);}
    else if(platform_start_seed(seed_sha,id,argv[2],input,argv[4],installed.sha,native,resolved,shell_sha))goto done;
    if(snprintf(start,sizeof start,"%s/%s",starts,id)>=(int)sizeof start||snprintf(domain,sizeof domain,"%s/generations/%s",updater_path,id)>=(int)sizeof domain)goto done;
    if(strlen(domain)+sizeof "/control">sizeof(((struct sockaddr_un *)0)->sun_path))goto done;
    FILE *f=open_memstream(&launch,&launch_size);if(!f)goto done;
    fprintf(f,"BROray-platform-launch/2\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n",argv[2],domain,id,input->manifest,native,resolved,shell_sha,input->operation,input->nonce);
    fwrite(manifest.bytes,1,manifest.size,f);
    if(fclose(f))goto done;digest_bytes(launch,launch_size,launch_sha);
    f=open_memstream(&transaction,&transaction_size);if(!f)goto done;
    fprintf(f,"BROray-platform-start-intent/1\nSTART_INTENT\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n",seed_sha,boot,argv[4],installed.sha,runtime_path,id,launch_sha,input->nonce);
    if(fclose(f))goto done;digest_bytes(transaction,transaction_size,transaction_sha);
    int bn=snprintf(binding,sizeof binding,"BROray-platform-start-binding/1\n%s\n%s\n",transaction_sha,launch_sha);
    if(bn<0||bn>=(int)sizeof binding)goto done;
    int bound=bg_exists(op,"platform-start.record"),directory=bg_exists(updaterfd,"starts");if(bound<0||directory<0||(bound&&!directory))goto done;
    if(directory){startsfd=checked_directory(starts);if(startsfd<0)goto done;}
    int present=startsfd>=0?bg_exists(startsfd,id):0;if(present<0||present!=bound)goto done;
    if(startsfd>=0&&platform_attempt_names(startsfd,0,platform_attempt_required(op,bound),id))goto done;
    if(observe){
        /* Pure derivation/verification for post-start context. This path may
         * neither create missing evidence nor acknowledge INSTALLING/COMMIT. */
        if(!bound)goto done;startfd=checked_directory(start);if(startfd<0)goto done;
        int logged=bg_exists(startfd,"supervisor.log"),host_logged=bg_exists(startfd,"service-host.log");if(logged<0||host_logged<0)goto done;
        int witnessed=bg_exists(startfd,"ledger-witnesses");if(witnessed<0||((logged||host_logged)&&!witnessed)||
           (witnessed&&platform_witness_directory(startfd,start,logged||host_logged)))goto done;
        const char *names[5]={"launch.record","transaction.record","ledger-witnesses"};int count=witnessed?3:2;
        if(logged)names[count++]="supervisor.log";if(host_logged)names[count++]="service-host.log";
        if(rc_names(startfd,names,count)||bg_record_exact(op,"platform-start.record",binding,(size_t)bn)||
           bg_record_exact(startfd,"transaction.record",transaction,transaction_size)||bg_record_exact(startfd,"launch.record",launch,launch_size))goto done;
        for(int i=witnessed?3:2;i<count;i++){struct stat st;int fd=openat(startfd,names[i],O_RDONLY|O_NOFOLLOW|O_CLOEXEC|O_NONBLOCK);if(fd<0)goto done;
            int bad=fstat(fd,&st)||!S_ISREG(st.st_mode)||st.st_uid!=geteuid()||st.st_nlink!=1||(st.st_mode&07777)!=0600;close(fd);if(bad)goto done;}
        goto verified;
    }
    /* External operation binding is durable before any lifetime directory is
     * created. A missing/partial pair on replay is retained, never rebuilt. */
    if(migration_record(op,"platform-start.record",binding,(size_t)bn,!bound))goto done;
    if(!directory){
        if(mkdirat(updaterfd,"starts",0700)||fsync(updaterfd))goto done;
        startsfd=checked_directory(starts);if(startsfd<0||bg_empty(startsfd))goto done;
    }
    if(!bound&&(mkdirat(startsfd,id,0700)||fsync(startsfd)))goto done;
    startfd=checked_directory(start);if(startfd<0||(!bound&&bg_empty(startfd)))goto done;
    int witnessed=bg_exists(startfd,"ledger-witnesses");if(witnessed<0||(witnessed&&platform_witness_directory(startfd,start,0)))goto done;
    const char *names[3]={"launch.record","transaction.record","ledger-witnesses"};int name_count=witnessed?3:2;
    if((bound&&rc_names(startfd,names,name_count))||
       migration_record(startfd,"transaction.record",transaction,transaction_size,!bound)||
       migration_record(startfd,"launch.record",launch,launch_size,!bound)||
       migration_sync_directory(start,startfd)||migration_sync_directory(starts,startsfd)||
       rc_names(startfd,names,name_count)||platform_attempt_names(startsfd,0,platform_attempt_required(op,1),id)||
       bg_record_exact(op,"platform-start.record",binding,(size_t)bn)||
       bg_record_exact(startfd,"transaction.record",transaction,transaction_size)||bg_record_exact(startfd,"launch.record",launch,launch_size)||
       service_interpreter_valid(shellfd,resolved,shell_sha)||runtime_names(runtimefd)||bg_runtime_valid(runtimefd,native)||
       bg_record_exact(runtimefd,"identity.json",runtime_record,(size_t)rn)||
       platform_install_apply(argv,platform_context_op(op),op_path,input,native,executor,held,statefd,shell,controller,1)||
       bg_record_exact(op,"platform-start.record",binding,(size_t)bn)||
       bg_record_exact(startfd,"transaction.record",transaction,transaction_size)||bg_record_exact(startfd,"launch.record",launch,launch_size))goto done;
verified:
    strcpy(ps.updater,updater_path);strcpy(ps.start,start);strcpy(ps.domain,domain);strcpy(ps.runtime,runtime_path);
    strcpy(ps.id,id);strcpy(ps.launch,launch_sha);strcpy(ps.transaction,transaction_sha);
    if(!observe&&!strcmp(argv[1],"recovery-start-intent"))printf("{\"ok\":true,\"phase\":\"START_INTENT\",\"generationId\":\"%s\",\"startIntentSha256\":\"%s\",\"transactionRecordSha256\":\"%s\",\"replayed\":%s,\"serviceStarted\":false,\"activationAllowed\":false}\n",id,launch_sha,transaction_sha,bound?"true":"false");result=0;
done:
    free(installed.bytes);free(manifest.bytes);free(launch);free(transaction);
    if(updaterfd>=0)close(updaterfd);if(runtimefd>=0)close(runtimefd);if(startsfd>=0)close(startsfd);if(startfd>=0)close(startfd);if(shellfd>=0)close(shellfd);if(fs>=0)close(fs);
    return result?migration_error("PLATFORM_START_INTENT_UNCONFIRMED"):0;
}

/* Authenticated post-start exception is confined to this exact transaction.
 * It reads historic retirement evidence without treating the new daemon's
 * projections as legacy objects. Unknown or merely live PIDs never qualify. */
static int platform_started_current(int op,const char *op_path,const char *root,const struct bg_input *input,const char *migration,const char *native){
    char shell[PATH_MAX],parent[PATH_MAX],marker[384],expected[256];
    const char *prefix=strcmp(root,"/")?root:"";
    if(snprintf(shell,sizeof shell,"%s/opt/bin/ash",prefix)>=(int)sizeof shell)return -1;
    char *args[]={"protected-start","recovery-start",(char*)root,(char*)input->operation,(char*)migration,(char*)input->nonce};
    if(platform_start_intent_apply(args,op,op_path,input,native,NULL,-1,-1,shell,NULL,1))return -1;
    int n=snprintf(marker,sizeof marker,"BROray-platform-starting/1\n%s\n%s\n%s\n",ps.launch,ps.transaction,boot);
    if(n<0||n>=(int)sizeof marker||bg_record_exact(op,"platform-starting.record",marker,(size_t)n)||
       snprintf(parent,sizeof parent,"%s/generations",ps.updater)>=(int)sizeof parent)return -1;
    int fd=checked_directory(parent);if(fd<0)return -1;
    int bad=platform_attempt_names(fd,1,platform_attempt_required(op,1),ps.id);close(fd);if(bad||pl_load(ps.start,ps.launch,ps.transaction,0))return -1;
    char *status[]={ps.runtime,"control",ps.domain,"STATUS",ps.id,(char*)input->manifest,(char*)input->operation,(char*)input->nonce};
    if(control_exchange(8,status,0)||!strstr(snapshot,"\"state\":\"RUNNING\",\"supervisedFromBirth\":true,")||
       !strstr(snapshot,",\"platformReady\":true,\"platformLaunch\":{\"contract\":\"broray-platform-launch/1\","))return -1;
    snprintf(expected,sizeof expected,"\"startIntentSha256\":\"%s\"",ps.launch);if(!strstr(snapshot,expected))return -1;
    snprintf(expected,sizeof expected,"\"transactionRecordSha256\":\"%s\"}",ps.transaction);if(!strstr(snapshot,expected))return -1;
    const char *u=strstr(snapshot,",\"updater\":");long pid;
    if(!u||sscanf(u,",\"updater\":{\"pid\":%ld,",&pid)!=1||pid<=1||pid>INT_MAX)return -1;
    struct identity now;if(capture((pid_t)pid,&now)||strcmp(now.exe,pl.shell))return -1;
    char *identity=NULL;size_t size=0;FILE *f=open_memstream(&identity,&size);if(!f)return -1;identity_json(f,&now);
    if(fclose(f)){free(identity);return -1;}bad=strncmp(u+11,identity,size);free(identity);if(bad)return -1;
    struct identity old=updater;updater=now;bad=pl_queue_ready();updater=old;
    return bad||pl_exact()||bg_record_exact(op,"platform-starting.record",marker,(size_t)n)?-1:0;
}

static int platform_host_intent(char **text,size_t *size){
    FILE *f=open_memstream(text,size);if(!f)return -1;
    fprintf(f,"BROray-platform-service-host-start/1\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n",boot,ps.launch,ps.transaction,pl.native,pl.host,pl.domain,pl.id,pl.manifest,pl.root,pl.shell,pl.shellsha);
    return fclose(f)?-1:0;
}
static int platform_host_scope(int op,struct identity *owner){
    char *intent=NULL,parent[PATH_MAX];size_t size=0;int result=-1,hosts=-1;
    if(pl_load(ps.start,ps.launch,ps.transaction,0)||platform_host_intent(&intent,&size)||
       bg_record_exact(op,"platform-host-starting.record",intent,size)||
       snprintf(parent,sizeof parent,"%s/hosts",ps.updater)>=(int)sizeof parent)goto done;
    hosts=checked_directory(parent);
    if(hosts<0||platform_attempt_names(hosts,0,platform_attempt_required(op,1),ps.id)||platform_host_identity(owner)||bg_record_exact(op,"platform-host-starting.record",intent,size))goto done;
    result=0;
done:free(intent);if(hosts>=0)close(hosts);return result;
}
static int platform_host_owner_text(const struct identity *owner,char **text,size_t *size){
    char *intent=NULL,sha[65];size_t intent_size=0;
    if(platform_host_intent(&intent,&intent_size)){free(intent);return -1;}
    digest_bytes(intent,intent_size,sha);free(intent);
    FILE *f=open_memstream(text,size);if(!f)return -1;
    fprintf(f,"BROray-platform-service-host-owner/1\n%s\n",sha);identity_json(f,owner);fputc('\n',f);
    return fclose(f)?-1:0;
}
static int platform_host_owner_record(int op,const struct identity *owner,int create){
    char *text=NULL,sha[65],anchor[128];size_t size=0;int result=-1;
    if(platform_host_owner_text(owner,&text,&size))goto done;
    digest_bytes(text,size,sha);int n=snprintf(anchor,sizeof anchor,"BROray-platform-service-host-owner-anchor/1\n%s\n",sha);
    if(n<0||n>=(int)sizeof anchor||bg_exists(op,"platform-host-owner.record.pending")!=0||
       bg_exists(op,"platform-host-owner.anchor.pending")!=0)goto done;
    if(create&&(bg_exists(op,"platform-host-owner.record")!=0||bg_exists(op,"platform-host-owner.anchor")!=0||
       migration_record(op,"platform-host-owner.anchor",anchor,(size_t)n,1)||migration_record(op,"platform-host-owner.record",text,size,1)))goto done;
    if(bg_record_exact(op,"platform-host-owner.record",text,size)||bg_record_exact(op,"platform-host-owner.anchor",anchor,(size_t)n)||fsync(op))goto done;
    result=0;
done:free(text);return result;
}
static char platform_verified_host_sha[65];
static int platform_host_proof(int op,int generation_bound){
    struct identity owner;platform_verified_host_sha[0]=0;
    if(platform_host_scope(op,&owner)||platform_host_owner_record(op,&owner,0))return -1;
    char sha[65],expected[128];char *args[]={pl.nativepath,"service-status",pl.host,pl.domain,pl.id,pl.manifest,pl.root,pl.shell,pl.shellsha,NULL};
    if(service_host_record_sha(args,&owner,sha))return -1;
    if(generation_bound){
        char *status[]={ps.runtime,"control",ps.domain,"STATUS",ps.id,pl.manifest,pl.op,pl.nonce};
        int n=snprintf(expected,sizeof expected,"\"serviceHostRecordSha256\":\"%s\"",sha);
        if(n<0||n>=(int)sizeof expected||control_exchange(8,status,0)||!strstr(snapshot,expected)||
           !strstr(snapshot,"\"state\":\"RUNNING\",\"supervisedFromBirth\":true,"))return -1;
    }
    strcpy(platform_verified_host_sha,sha);return 0;
}
/* Called only on the fresh protected STARTING path. Partial launch evidence
 * is retained, never adopted or retried as a new independent host. */
static int platform_host_start(int op,int updaterfd,int startfd,int runtimefd){
    char *intent=NULL,parent[PATH_MAX];size_t size=0;int hosts=-1,log=-1,result=-1;int parent_present=bg_exists(updaterfd,"hosts");
    if(parent_present<0||pl_load(ps.start,ps.launch,ps.transaction,0)||platform_host_intent(&intent,&size)||
       bg_exists(op,"platform-host-starting.record")!=0||
       bg_exists(op,"platform-host-owner.record")!=0||bg_exists(op,"platform-host-owner.anchor")!=0||
       bg_exists(startfd,"service-host.log")!=0||
       snprintf(parent,sizeof parent,"%s/hosts",ps.updater)>=(int)sizeof parent)goto done;
    if(parent_present){hosts=checked_directory(parent);if(hosts<0||platform_attempt_names(hosts,0,0,ps.id))goto done;}
    if(migration_record(op,"platform-host-starting.record",intent,size,1)||(!parent_present&&(mkdirat(updaterfd,"hosts",0700)||fsync(updaterfd))))goto done;
    if(hosts<0)hosts=checked_directory(parent);
    if(hosts<0||platform_attempt_names(hosts,0,0,ps.id)||mkdirat(hosts,ps.id,0700)||fsync(hosts))goto done;
    log=openat(startfd,"service-host.log",O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW|O_CLOEXEC,0600);
    if(log<0||fsync(log)||fsync(startfd)||bg_record_exact(op,"platform-host-starting.record",intent,size))goto done;
    pid_t child=fork();if(child<0)goto done;
    if(!child){
        if(setsid()<0||dup2(log,1)<0||dup2(log,2)<0)_exit(74);
        int null=open("/dev/null",O_RDONLY|O_CLOEXEC);if(null<0||dup2(null,0)<0)_exit(74);
        service_close_fds(runtimefd,-1);if(clearenv())_exit(74);
        char *command[]={ps.runtime,"service-host",pl.host,pl.domain,pl.id,pl.manifest,pl.root,pl.shell,pl.shellsha,NULL};
        extern char **environ;fexecve(runtimefd,command,environ);_exit(74);
    }
    close(log);log=-1;
    uint64_t until=millis()+2000;
    while(millis()<until){
        int status;pid_t ended=waitpid(child,&status,WNOHANG);if(ended==child||ended<0)goto done;
        /* Polling bounds latency only. Success requires authenticated native
         * peer + exact scope, immutable host record and interpreter proof. */
        struct identity owner;
        if(!platform_host_scope(op,&owner)){
            /* The authenticated native peer must be our still-unreaped fork,
             * not another same-executable process at a replacement pathname.
             * Pin its complete identity durably before updater launch. */
            if(owner.pid!=child||waitpid(child,&status,WNOHANG)!=0||
               platform_host_owner_record(op,&owner,1)||platform_host_proof(op,0))goto done;
            result=0;break;
        }
        struct timespec pause={0,20000000L};nanosleep(&pause,NULL);
    }
done:free(intent);if(hosts>=0)close(hosts);if(log>=0)close(log);return result;
}

static int platform_start_apply(char **argv,int op,const char *op_path,const struct bg_input *input,const char *native,const struct identity *executor,int held,int statefd,const char *shell,const char *controller){
    char parent[PATH_MAX],marker[384];int root=-1,gen=-1,start=-1,runfd=-1,log=-1,result=75,replay=0;
    const char *stage="intent";uint64_t wait_begin=0,poll_ms=0;unsigned polls=0;
    int intent=bg_exists(op,"platform-start.record");if(intent<0)goto done;
    if(platform_start_intent_apply(argv,op,op_path,input,native,executor,held,statefd,shell,controller,intent||platform_attempt_index(op)==0))goto done;
    root=checked_directory(ps.updater);if(root<0)goto done;
    int parent_present=bg_exists(root,"generations"),exists=0,begun=bg_exists(op,"platform-starting.record");
    if(parent_present<0||begun<0||snprintf(parent,sizeof parent,"%s/generations",ps.updater)>=(int)sizeof parent)goto done;
    if(parent_present){gen=checked_directory(parent);if(gen<0)goto done;exists=bg_exists(gen,ps.id);}
    if(exists<0||exists!=begun)goto done;
    if(exists){
        stage="replay-proof";
        if(platform_started_current(op,op_path,argv[2],input,argv[4],native)||
           platform_install_apply(argv,platform_context_op(op),op_path,input,native,executor,held,statefd,shell,controller,1))goto done;
        replay=1;
    }else{
        stage="launch-proof";
        /* A new retry intent was already durably created and fully checked
         * above. Recheck its exact records here; do not execute the complete
         * install/context preparation twice in the same launch command. */
        if(!service_untraced()||platform_start_intent_apply(argv,op,op_path,input,native,executor,held,statefd,shell,controller,!intent))goto done;
        start=checked_directory(ps.start);if(start<0||bg_exists(start,"supervisor.log")!=0)goto done;
        runfd=open(ps.runtime,O_RDONLY|O_NOFOLLOW|O_CLOEXEC);char sha[65];
        if(runfd<0||hash_fd(runfd,sha)||strcmp(sha,native))goto done;
        int n=snprintf(marker,sizeof marker,"BROray-platform-starting/1\n%s\n%s\n%s\n",ps.launch,ps.transaction,boot);
        if(n<0||n>=(int)sizeof marker||migration_record(op,"platform-starting.record",marker,(size_t)n,1)||
           (!parent_present&&(mkdirat(root,"generations",0700)||fsync(root))))goto done;
        if(gen<0)gen=checked_directory(parent);
        if(gen<0||(parent_present?platform_attempt_names(gen,1,0,ps.id):bg_empty(gen))||mkdirat(gen,ps.id,0700)||fsync(gen))goto done;
        log=openat(start,"supervisor.log",O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW|O_CLOEXEC,0600);
        if(log<0||fsync(log)||fsync(start))goto done;
        stage="service-host-start";if(platform_host_start(op,root,start,runfd))goto done;
        char host_sha[65];if(!hex64(platform_verified_host_sha))goto done;strcpy(host_sha,platform_verified_host_sha);
        pid_t child=fork();if(child<0)goto done;
        if(!child){
            if(setsid()<0||dup2(log,1)<0||dup2(log,2)<0)_exit(74);
            int null=open("/dev/null",O_RDONLY|O_CLOEXEC);if(null<0||dup2(null,0)<0)_exit(74);
            service_close_fds(runfd,-1);if(clearenv())_exit(74);
            char *command[]={ps.runtime,"run",ps.domain,ps.id,(char*)input->manifest,"--",ps.runtime,"platform-daemon",ps.start,ps.launch,ps.transaction,host_sha,NULL};
            extern char **environ;fexecve(runfd,command,environ);_exit(74);
        }
        close(log);log=-1;
        /* Poll only authenticated readiness of this already-created generation.
         * Timeout grants no stop/cleanup authority and never causes a relaunch. */
        wait_begin=millis();uint64_t until=wait_begin+60000;int ready=0;stage="readiness-wait";
        char *status_args[]={ps.runtime,"control",ps.domain,"STATUS",ps.id,(char*)input->manifest,(char*)input->operation,(char*)input->nonce};
        while(millis()<until){
            int status;pid_t got=waitpid(child,&status,WNOHANG);if(got==child||got<0){stage="supervisor-exit";goto done;}
            /* This projection is only a wake-up hint, never readiness proof.
             * A STATUS request makes the tracer synchronously verify its full
             * ledger/platform. Repeating it before the daemon has published
             * anything competes with the syscall stops needed to get there.
             * An early/forged hint still requires the authenticated generation
             * response and all exact transaction/context/bytes checks below. */
            int hint=bg_exists(root,"daemon.ready");if(hint<0)goto done;
            if(hint){
                uint64_t polled=millis();int rc=control_exchange(8,status_args,0);poll_ms+=millis()-polled;polls++;
                if(!rc&&strstr(snapshot,",\"platformReady\":true,\"platformLaunch\":{\"contract\":\"broray-platform-launch/1\",")){ready=1;break;}
            }
            struct timespec pause={0,50000000L};nanosleep(&pause,NULL);
        }
        fprintf(stderr,"PLATFORM_START_WAIT ready=%d elapsed_ms=%llu polls=%u poll_ms=%llu\n",ready,(unsigned long long)(millis()-wait_begin),polls,(unsigned long long)poll_ms);
        if(!ready)goto done;
        stage="live-proof";if(platform_started_current(op,op_path,argv[2],input,argv[4],native))goto done;
        stage="installed-proof";if(platform_install_apply(argv,platform_context_op(op),op_path,input,native,executor,held,statefd,shell,controller,1))goto done;
    }
    stage="final-live-proof";
    if(platform_started_current(op,op_path,argv[2],input,argv[4],native)||platform_host_proof(op,1))goto done;
    printf("{\"ok\":true,\"phase\":\"READY\",\"generationId\":\"%s\",\"startIntentSha256\":\"%s\",\"transactionRecordSha256\":\"%s\",\"replayed\":%s,\"serviceStarted\":true,\"activationAllowed\":false}\n",ps.id,ps.launch,ps.transaction,replay?"true":"false");result=0;
done:
    if(result)fprintf(stderr,"PLATFORM_START_DETAIL stage=%s elapsed_ms=%llu polls=%u poll_ms=%llu\n",stage,(unsigned long long)(wait_begin?millis()-wait_begin:0),polls,(unsigned long long)poll_ms);
    if(root>=0)close(root);if(gen>=0)close(gen);if(start>=0)close(start);if(runfd>=0)close(runfd);if(log>=0)close(log);
    return result?migration_error("PLATFORM_START_NEEDS_RECOVERY"):0;
}

/* Bind stable process identities from the authenticated native snapshot, not
 * its changing revision/child list. The exact installed and live proofs must
 * have succeeded immediately before deriving this deterministic receipt. */
static int platform_commit_text(char **argv,const struct bg_input *input,const char *native,char **record,size_t *size){
    const char *identity=strstr(snapshot,",\"supervisor\":");
    const char *end=identity?strstr(identity,",\"stopOperationId\":"):NULL;
    if(!identity||!end||end<=identity||!strstr(identity,",\"updater\":{\"pid\":"))return -1;
    FILE *f=open_memstream(record,size);if(!f)return -1;
    fprintf(f,"{\"schemaVersion\":1,\"contract\":\"broray-platform-commit/1\",\"phase\":\"COMMITTED\",\"operationId\":\"%s\",\"stopNonce\":\"%s\",\"bootId\":\"%s\",\"migrationIntentSha256\":\"%s\",\"platformManifestSha256\":\"%s\",\"nativeSha256\":\"%s\",\"generationId\":\"%s\",\"startIntentSha256\":\"%s\",\"transactionRecordSha256\":\"%s\"",input->operation,input->nonce,boot,argv[4],input->manifest,native,ps.id,ps.launch,ps.transaction);
    fwrite(identity,1,(size_t)(end-identity),f);
    fputs(",\"platformReady\":true,\"activationAllowed\":false}\n",f);
    return fclose(f)?-1:0;
}

/* Commit only an already proven generation. This phase neither starts a
 * service nor releases the operation fence or authorizes application work. */
static int platform_commit_stop_clear(int op){
    const char *names[]={"platform-stop-current.intent","platform-stop-current.intent-anchor","platform-stop-current.receipt","platform-stop-current.receipt-anchor"};
    for(unsigned i=0;i<sizeof names/sizeof names[0];i++){
        char pending[128];int n=snprintf(pending,sizeof pending,"%s.pending",names[i]);
        if(n<0||n>=(int)sizeof pending||bg_exists(op,names[i])!=0||bg_exists(op,pending)!=0)return -1;
    }
    return 0;
}
static int platform_commit_apply(char **argv,int op,const char *op_path,const struct bg_input *input,const char *native,const struct identity *executor,int held,int statefd,const char *shell,const char *controller){
    char *record=NULL,*again=NULL;size_t size=0,again_size=0;char sha[65],anchor[128];int result=75,check=!strcmp(argv[1],"recovery-commit-check");
    if(platform_commit_stop_clear(op)||platform_install_apply(argv,platform_context_op(op),op_path,input,native,executor,held,statefd,shell,controller,1)||
       platform_started_current(op,op_path,argv[2],input,argv[4],native)||platform_host_proof(op,1)||
       platform_commit_text(argv,input,native,&record,&size))goto done;
    int present=bg_exists(op,"platform-committed.record"),bound=bg_exists(op,"platform-committed.anchor");
    if(present<0||bound<0||present!=bound||(check&&!present)||bg_exists(op,"platform-committed.record.pending")!=0||
       bg_exists(op,"platform-committed.anchor.pending")!=0)goto done;
    digest_bytes(record,size,sha);int n=snprintf(anchor,sizeof anchor,"BROray-platform-committed-anchor/1\n%s\n",sha);
    if(n<0||n>=(int)sizeof anchor||platform_commit_stop_clear(op)||
       (!check&&migration_record(op,"platform-committed.anchor",anchor,(size_t)n,!present))||
       (!check&&migration_record(op,"platform-committed.record",record,size,!present))||
       bg_record_exact(op,"platform-committed.anchor",anchor,(size_t)n)||
       bg_record_exact(op,"platform-committed.record",record,size)||(!check&&fsync(op)))goto done;
    /* No receipt-only success, including lost replies: recheck exact platform,
     * live generation, queue and both identities after durable publication. */
    if(platform_commit_stop_clear(op)||platform_install_apply(argv,platform_context_op(op),op_path,input,native,executor,held,statefd,shell,controller,1)||
       platform_started_current(op,op_path,argv[2],input,argv[4],native)||platform_host_proof(op,1)||
       platform_commit_text(argv,input,native,&again,&again_size)||again_size!=size||memcmp(again,record,size)||
       bg_record_exact(op,"platform-committed.anchor",anchor,(size_t)n)||
       bg_record_exact(op,"platform-committed.record",record,size)||platform_commit_stop_clear(op))goto done;
    printf("{\"ok\":true,\"phase\":\"%s\",\"generationId\":\"%s\",\"commitReceiptSha256\":\"%s\",\"replayed\":%s,\"platformReady\":true,\"activationAllowed\":false}\n",check?"COMMIT_VERIFIED":"COMMITTED",ps.id,sha,present?"true":"false");result=0;
done:
    free(record);free(again);return result?migration_error("PLATFORM_COMMIT_UNCONFIRMED"):0;
}

/* Protected stop binds stable identities before the native lifetime owner is
 * asked to drain. It never signals a discovered PID, removes a generation, or
 * uses elapsed time/process absence as evidence that writers have stopped. */
static int psc_intent_text(const struct bg_input *input,const char *migration,const char *native,char **text,size_t *size){
    const char *identity=strstr(snapshot,",\"supervisor\":");
    const char *end=identity?strstr(identity,",\"stopOperationId\":"):NULL;
    if(!identity||!end||end<=identity||!strstr(identity,",\"updater\":{\"pid\":"))return -1;
    FILE *f=open_memstream(text,size);if(!f)return -1;
    fprintf(f,"BROray-platform-stop-current/1\nSTOP_INTENT\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n",input->operation,input->nonce,boot,migration,input->manifest,native,ps.id,ps.launch,ps.transaction);
    fwrite(identity,1,(size_t)(end-identity),f);fputc('\n',f);return fclose(f)?-1:0;
}
static int psc_pair(int op,const char *name,const char *anchor){
    char pending[128];int a=bg_exists(op,name),b=bg_exists(op,anchor);
    if(a<0||b<0||a!=b)return -1;
    const char *names[]={name,anchor};for(unsigned i=0;i<2;i++){
        int n=snprintf(pending,sizeof pending,"%s.pending",names[i]);if(n<0||n>=(int)sizeof pending||bg_exists(op,pending)!=0)return -1;
    }
    return a;
}
static int psc_terminal(const struct bg_input *input){
    char field[160];
    if(!strstr(snapshot,"\"state\":\"STOPPED\",\"supervisedFromBirth\":true,")||
       !strstr(snapshot,"\"children\":[],\"awaitingBirth\":[],\"exitedUnreaped\":[]"))return -1;
    snprintf(field,sizeof field,"\"stopOperationId\":\"%s\",\"stopNonce\":\"%s\",",input->operation,input->nonce);
    return strstr(snapshot,field)?0:-1;
}
static int psc_retired(const struct bg_input *input){
    int fd=checked_directory(ps.domain);if(fd<0)return -1;struct retired_record r;char name[64],*bytes=NULL;size_t size=0;
    int bad=retirement_valid(fd,ps.domain,&r)||strcmp(r.gen,ps.id)||strcmp(r.sha,input->manifest)||strcmp(r.op,input->operation)||strcmp(r.nonce,input->nonce);
    if(!bad){record_name(r.total,name);bad=safe_bytes_at(fd,name,&bytes,&size)||size>=sizeof snapshot||memchr(bytes,0,size);}
    close(fd);if(!bad){memcpy(snapshot,bytes,size);snapshot[size]=0;char expected[384];
        int n=snprintf(expected,sizeof expected,"{\"schemaVersion\":2,\"contract\":\"broray-updater-generation/2\",\"generationId\":\"%s\",\"platformManifestSha256\":\"%s\",\"bootId\":\"%s\",",ps.id,input->manifest,boot);
        bad=n<0||n>=(int)sizeof expected||size<(size_t)n||memcmp(snapshot,expected,(size_t)n)||psc_terminal(input);
    }
    free(bytes);return bad?-1:0;
}
/* The original host exits itself only after publishing complete terminal
 * evidence. Keep its inode-bound lifetime lock through STOPPED publication.
 * This historical proof also works after platform rollback; it does not load
 * or execute the newly installed daemon. No PID is signalled or adopted. */
static int platform_host_retired(int op,const char *root,const struct bg_input *input,const char *native,int *held,uint64_t until,char receipt_sha[65]){
    char parent[PATH_MAX],host_path[PATH_MAX],shell_path[PATH_MAX],shell[PATH_MAX],shell_sha[65],terminal[512],sha[65],anchor[128];
    int hosts=-1,host=*held,fs=-1,ash=-1,result=-1;char *record=NULL,*scope=NULL,*intent=NULL,*owner=NULL;size_t size=0,scope_size=0,intent_size=0,owner_size=0;
    const char *prefix=strcmp(root,"/")?root:"";
    if(snprintf(parent,sizeof parent,"%s/hosts",ps.updater)>=(int)sizeof parent||
       snprintf(host_path,sizeof host_path,"%s/%s",parent,ps.id)>=(int)sizeof host_path||
       snprintf(shell_path,sizeof shell_path,"%s/opt/bin/ash",prefix)>=(int)sizeof shell_path||!realpath(shell_path,shell))goto done;
    hosts=checked_directory(parent);if(hosts<0||platform_attempt_names(hosts,0,platform_attempt_required(op,1),ps.id))goto done;
    if(host<0){
        host=checked_directory(host_path);if(host<0)goto done;
        while(flock(host,LOCK_EX|LOCK_NB)){if(errno!=EWOULDBLOCK||millis()>=until)goto done;struct timespec pause={0,10000000L};nanosleep(&pause,NULL);}
    }
    struct stat st,named;
    if(fstat(host,&st)||fstatat(hosts,ps.id,&named,AT_SYMLINK_NOFOLLOW)||!S_ISDIR(st.st_mode)||
       st.st_uid!=geteuid()||(st.st_mode&07777)!=0700||named.st_dev!=st.st_dev||named.st_ino!=st.st_ino||named.st_mode!=st.st_mode||named.st_uid!=st.st_uid)goto done;
    fs=migration_directory("/");if(fs<0)goto done;ash=migration_relative(fs,shell+1);
    if(ash<0||hash_fd(ash,shell_sha)||service_interpreter_valid(ash,shell,shell_sha)||safe_bytes_at(host,"host.record",&record,&size)||memchr(record,0,size))goto done;
    char *args[]={(char*)native,"service-host",host_path,ps.domain,ps.id,(char*)input->manifest,(char*)root,shell,shell_sha};
    FILE *f=open_memstream(&scope,&scope_size);if(!f)goto done;
    fputs("BROray-independent-app-service/1\n",f);for(int i=2;i<9;i++)fprintf(f,"%s\n",args[i]);
    if(fclose(f)||size<=scope_size+1||memcmp(record,scope,scope_size)||record[scope_size]!='{'||record[size-1]!='\n'||memchr(record+scope_size,'\n',size-scope_size-1))goto done;
    f=open_memstream(&intent,&intent_size);if(!f)goto done;
    fprintf(f,"BROray-platform-service-host-start/1\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n",boot,ps.launch,ps.transaction,native,host_path,ps.domain,ps.id,input->manifest,root,shell,shell_sha);
    if(fclose(f)||bg_record_exact(op,"platform-host-starting.record",intent,intent_size))goto done;
    digest_bytes(intent,intent_size,sha);f=open_memstream(&owner,&owner_size);if(!f)goto done;
    fprintf(f,"BROray-platform-service-host-owner/1\n%s\n",sha);fwrite(record+scope_size,1,size-scope_size,f);
    if(fclose(f))goto done;digest_bytes(owner,owner_size,sha);
    int n=snprintf(anchor,sizeof anchor,"BROray-platform-service-host-owner-anchor/1\n%s\n",sha);
    if(n<0||n>=(int)sizeof anchor||bg_exists(op,"platform-host-owner.record.pending")!=0||bg_exists(op,"platform-host-owner.anchor.pending")!=0||
       bg_record_exact(op,"platform-host-owner.record",owner,owner_size)||bg_record_exact(op,"platform-host-owner.anchor",anchor,(size_t)n)||
       service_retirement_text(host,args,record,size,terminal)||migration_record(host,"retirement.receipt",terminal,strlen(terminal),0)||
       fstatat(hosts,ps.id,&named,AT_SYMLINK_NOFOLLOW)||named.st_dev!=st.st_dev||named.st_ino!=st.st_ino||named.st_mode!=st.st_mode||named.st_uid!=st.st_uid)goto done;
    digest_bytes(terminal,strlen(terminal),receipt_sha);*held=host;host=-1;result=0;
done:
    free(record);free(scope);free(intent);free(owner);if(host>=0&&host!=*held)close(host);if(hosts>=0)close(hosts);if(fs>=0)close(fs);if(ash>=0)close(ash);return result;
}
/* Historical STOPPED proof deliberately does not assert that live platform
 * files still contain the new manifest: rollback validates those separately.
 * Complete immutable stop and retirement evidence is mandatory; no live PID,
 * empty namespace or lock availability can substitute for this proof. */
static int platform_stopped_current_proof(int op,const char *op_path,const char *root,const struct bg_input *input,const char *migration,const char *native){
    int parent=-1,domain=-1,host=-1,result=-1;char shell[PATH_MAX],path[PATH_MAX],marker[384],expected[768],host_sha[65];
    char *intent=NULL;size_t size=0;char sha[65],anchor[128],terminal[65],receipt[512],receipt_sha[65],receipt_anchor[128];
    struct migration_file retired;memset(&retired,0,sizeof retired);const char *prefix=strcmp(root,"/")?root:"";
    if(snprintf(shell,sizeof shell,"%s/opt/bin/ash",prefix)>=(int)sizeof shell)return -1;
    char *args[]={"protected-history","recovery-rollback",(char*)root,(char*)input->operation,(char*)migration,(char*)input->nonce};
    if(platform_start_intent_apply(args,op,op_path,input,native,NULL,-1,-1,shell,NULL,1)||
       psc_pair(op,"platform-stop-current.intent","platform-stop-current.intent-anchor")!=1||
       psc_pair(op,"platform-stop-current.receipt","platform-stop-current.receipt-anchor")!=1)goto done;
    int n=snprintf(marker,sizeof marker,"BROray-platform-starting/1\n%s\n%s\n%s\n",ps.launch,ps.transaction,boot);
    if(n<0||n>=(int)sizeof marker||bg_record_exact(op,"platform-starting.record",marker,(size_t)n)||
       snprintf(path,sizeof path,"%s/generations",ps.updater)>=(int)sizeof path)goto done;
    parent=checked_directory(path);if(parent<0)goto done;
    if(platform_attempt_names(parent,1,platform_attempt_required(op,1),ps.id)||psc_retired(input))goto done;
    n=snprintf(expected,sizeof expected,"\"platformReady\":false,\"platformLaunch\":{\"contract\":\"broray-platform-launch/1\",\"startIntentSha256\":\"%s\",\"daemonSha256\":\"%s\",\"nativeSha256\":\"%s\",",ps.launch,input->after[5],native);
    if(n<0||n>=(int)sizeof expected||!strstr(snapshot,expected))goto done;
    n=snprintf(expected,sizeof expected,"\"operationId\":\"%s\",\"stopNonce\":\"%s\",\"transactionRecordSha256\":\"%s\"}",input->operation,input->nonce,ps.transaction);
    if(n<0||n>=(int)sizeof expected||!strstr(snapshot,expected)||psc_intent_text(input,migration,native,&intent,&size))goto done;
    digest_bytes(intent,size,sha);n=snprintf(anchor,sizeof anchor,"BROray-platform-stop-intent-anchor/1\n%s\n",sha);
    if(n<0||n>=(int)sizeof anchor||bg_record_exact(op,"platform-stop-current.intent",intent,size)||
       bg_record_exact(op,"platform-stop-current.intent-anchor",anchor,(size_t)n))goto done;
    domain=checked_directory(ps.domain);if(domain<0||migration_read(domain,"retirement.receipt",&retired,0)||retired.mode!=0600)goto done;
    digest_bytes(snapshot,strlen(snapshot),terminal);
    if(platform_host_retired(op,root,input,native,&host,0,host_sha))goto done;
    int rn=snprintf(receipt,sizeof receipt,"BROray-platform-stop-current-receipt/2\nSTOPPED\n%s\n%s\n%s\n%s\n",sha,retired.sha,terminal,host_sha);
    if(rn<0||rn>=(int)sizeof receipt)goto done;digest_bytes(receipt,(size_t)rn,receipt_sha);
    int an=snprintf(receipt_anchor,sizeof receipt_anchor,"BROray-platform-stop-current-anchor/1\n%s\n",receipt_sha);
    if(an<0||an>=(int)sizeof receipt_anchor||bg_record_exact(op,"platform-stop-current.receipt",receipt,(size_t)rn)||
       bg_record_exact(op,"platform-stop-current.receipt-anchor",receipt_anchor,(size_t)an)||
       bg_record_exact(domain,"retirement.receipt",retired.bytes,retired.size))goto done;
    result=0;
done:
    free(intent);free(retired.bytes);if(parent>=0)close(parent);if(domain>=0)close(domain);if(host>=0)close(host);return result;
}
/* Retain installation exclusion while old platform inodes are restored.
 * The lock alone is not a no-writers proof; complete terminal history is
 * independently required on admission and at each rollback file boundary. */
static int platform_rollback_generation_guard(int op,const char *op_path,const char *root,const struct bg_input *input,const char *migration,const char *native,int *held,int acquire){
    char path[PATH_MAX];const char *prefix=strcmp(root,"/")?root:"";int updaterfd=-1,parent=-1,fd=-1,result=-1;
    if(platform_attempt_index(op)!=pa.count&&
       (acquire||*held<0||platform_attempt_index(op)!=pa.count-1||platform_attempt_prestart(pa.node[pa.count].fd,op_path,root,input,migration,native)))return -1;
    int proof_op=op;
    if(platform_attempt_index(op)>0&&!platform_attempt_prestart(op,op_path,root,input,migration,native))proof_op=pa.node[pa.count-1].fd;
    if(snprintf(path,sizeof path,"%s/opt/var/lib/broray-updater",prefix)>=(int)sizeof path)return -1;
    updaterfd=checked_directory(path);if(updaterfd<0)return -1;int exists=bg_exists(updaterfd,"generations");if(exists<0)goto done;
    if(!exists){
        /* Before-start rollback is still allowed, but vanished generation
         * evidence after a launch/stop is never interpreted as absence. */
        if(*held>=0||bg_exists(op,"platform-starting.record")!=0||bg_exists(op,"platform-starting.record.pending")!=0||platform_commit_stop_clear(op))goto done;
        result=0;goto done;
    }
    if(platform_stopped_current_proof(proof_op,op_path,root,input,migration,native)||
       snprintf(path,sizeof path,"%s/generations",ps.updater)>=(int)sizeof path)goto done;
    parent=checked_directory(path);if(parent<0)goto done;
    if(acquire){if(*held>=0)goto done;fd=openat(parent,".generation-lifetime.lock",O_RDWR|O_NOFOLLOW|O_CLOEXEC);}
    else fd=*held;
    struct stat st,named;
    if(fd<0||fstat(fd,&st)||!S_ISREG(st.st_mode)||st.st_uid!=geteuid()||st.st_nlink!=1||(st.st_mode&07777)!=0600||st.st_size||
       (acquire&&flock(fd,LOCK_EX|LOCK_NB))||fstatat(parent,".generation-lifetime.lock",&named,AT_SYMLINK_NOFOLLOW)||
       named.st_dev!=st.st_dev||named.st_ino!=st.st_ino||named.st_mode!=st.st_mode||named.st_uid!=st.st_uid||named.st_nlink!=1||named.st_size||
       platform_stopped_current_proof(proof_op,op_path,root,input,migration,native))goto done;
    if(acquire){*held=fd;fd=-1;}result=0;
done:
    if(acquire&&fd>=0)close(fd);if(parent>=0)close(parent);if(updaterfd>=0)close(updaterfd);return result;
}
static int platform_stop_current_proof(int op,const char *op_path,const char *root,const struct bg_input *input,const char *migration,const char *native){
    char shell[PATH_MAX],parent[PATH_MAX],marker[384],anchor[128],sha[65];char *intent=NULL;size_t size=0;int fd=-1,result=-1;
    const char *prefix=strcmp(root,"/")?root:"";
    if(snprintf(shell,sizeof shell,"%s/opt/bin/ash",prefix)>=(int)sizeof shell)return -1;
    char *args[]={"protected-stop","recovery-stop-current",(char*)root,(char*)input->operation,(char*)migration,(char*)input->nonce};
    if(platform_start_intent_apply(args,op,op_path,input,native,NULL,-1,-1,shell,NULL,1))goto done;
    int n=snprintf(marker,sizeof marker,"BROray-platform-starting/1\n%s\n%s\n%s\n",ps.launch,ps.transaction,boot);
    if(n<0||n>=(int)sizeof marker||bg_record_exact(op,"platform-starting.record",marker,(size_t)n)||
       snprintf(parent,sizeof parent,"%s/generations",ps.updater)>=(int)sizeof parent)goto done;
    fd=checked_directory(parent);if(fd<0)goto done;
    if(platform_attempt_names(fd,1,platform_attempt_required(op,1),ps.id)||pl_load(ps.start,ps.launch,ps.transaction,0))goto done;
    int present=psc_pair(op,"platform-stop-current.intent","platform-stop-current.intent-anchor");if(present<0)goto done;
    int domain=checked_directory(ps.domain);if(domain<0)goto done;int retired=bg_exists(domain,"retirement.receipt");close(domain);if(retired<0)goto done;
    if(retired){if(!present||psc_retired(input))goto done;}
    else if(!present){
        /* A root can fail before READY while its lifetime supervisor drains
         * every writer. Admit only that authenticated, unbound terminal proof;
         * live unready/unknown generations remain protected. STOP will bind
         * the durable intent without sending a signal to an empty domain. */
        if(platform_started_current(op,op_path,root,input,migration,native)){
            char *status[]={ps.runtime,"control",ps.domain,"STATUS",ps.id,(char*)input->manifest,(char*)input->operation,(char*)input->nonce};
            char expected[1024];
            if(control_exchange(8,status,0)||!strstr(snapshot,"\"state\":\"STOPPED\",\"supervisedFromBirth\":true,")||
               !strstr(snapshot,"\"children\":[],\"awaitingBirth\":[],\"exitedUnreaped\":[]")||
               !strstr(snapshot,"\"stopOperationId\":\"\",\"stopNonce\":\"\","))goto done;
            int en=snprintf(expected,sizeof expected,"\"platformReady\":false,\"platformLaunch\":{\"contract\":\"broray-platform-launch/1\",\"startIntentSha256\":\"%s\",\"daemonSha256\":\"%s\",\"nativeSha256\":\"%s\",\"interpreterSha256\":\"%s\",\"operationId\":\"%s\",\"stopNonce\":\"%s\",\"transactionRecordSha256\":\"%s\"}",ps.launch,input->after[5],native,pl.shellsha,input->operation,input->nonce,ps.transaction);
            if(en<0||en>=(int)sizeof expected||!strstr(snapshot,expected))goto done;
        }
    }else{
        char *status[]={ps.runtime,"control",ps.domain,"STATUS",ps.id,(char*)input->manifest,(char*)input->operation,(char*)input->nonce};
        if(control_exchange(8,status,0)||!strstr(snapshot,"\"supervisedFromBirth\":true,"))goto done;
        char expected[256];snprintf(expected,sizeof expected,"\"startIntentSha256\":\"%s\"",ps.launch);if(!strstr(snapshot,expected))goto done;
        snprintf(expected,sizeof expected,"\"transactionRecordSha256\":\"%s\"}",ps.transaction);if(!strstr(snapshot,expected))goto done;
    }
    if(psc_intent_text(input,migration,native,&intent,&size))goto done;
    digest_bytes(intent,size,sha);int an=snprintf(anchor,sizeof anchor,"BROray-platform-stop-intent-anchor/1\n%s\n",sha);
    if(an<0||an>=(int)sizeof anchor||(present&&(bg_record_exact(op,"platform-stop-current.intent",intent,size)||bg_record_exact(op,"platform-stop-current.intent-anchor",anchor,(size_t)an)))||pl_exact())goto done;
    result=0;
done:
    if(fd>=0)close(fd);free(intent);return result;
}
static int platform_stop_current_apply(char **argv,int op,const char *op_path,const struct bg_input *input,const char *native,const struct identity *executor,int held,int statefd,const char *shell,const char *controller){
    int result=75,parent=-1,lifetime=-1,domain=-1,host=-1;char *intent=NULL;size_t size=0;
    char sha[65],anchor[128],path[PATH_MAX],receipt[512],receipt_sha[65],receipt_anchor[128];struct migration_file retirement;memset(&retirement,0,sizeof retirement);
    int present=psc_pair(op,"platform-stop-current.intent","platform-stop-current.intent-anchor"),complete=psc_pair(op,"platform-stop-current.receipt","platform-stop-current.receipt-anchor");
    if(present<0||complete<0||(complete&&!present)||
       platform_install_apply(argv,platform_context_op(op),op_path,input,native,executor,held,statefd,shell,controller,1)||
       platform_stop_current_proof(op,op_path,argv[2],input,argv[4],native)||psc_intent_text(input,argv[4],native,&intent,&size))goto done;
    digest_bytes(intent,size,sha);int an=snprintf(anchor,sizeof anchor,"BROray-platform-stop-intent-anchor/1\n%s\n",sha);
    if(an<0||an>=(int)sizeof anchor||migration_record(op,"platform-stop-current.intent-anchor",anchor,(size_t)an,!present)||
       migration_record(op,"platform-stop-current.intent",intent,size,!present)||
       platform_stop_current_proof(op,op_path,argv[2],input,argv[4],native))goto done;
    domain=checked_directory(ps.domain);if(domain<0)goto done;int retired=bg_exists(domain,"retirement.receipt");if(retired<0)goto done;
    char *control[]={ps.runtime,"control",ps.domain,"STATUS",ps.id,(char*)input->manifest,(char*)input->operation,(char*)input->nonce};
    if(!retired){
        if(complete)goto done;
        if(strstr(snapshot,"\"state\":\"RUNNING\"")||strstr(snapshot,"\"stopOperationId\":\"\",\"stopNonce\":\"\"")){
            control[3]="STOP";if(control_exchange(8,control,0))goto done;
        }
        uint64_t until=millis()+5000;control[3]="STATUS";
        while(psc_terminal(input)){
            if(millis()>=until||control_exchange(8,control,0)||platform_stop_current_proof(op,op_path,argv[2],input,argv[4],native))goto done;
            if(psc_terminal(input)){struct timespec pause={0,50000000L};nanosleep(&pause,NULL);}
        }
        control[3]="RETIRE";if(control_exchange(8,control,0)||psc_retired(input))goto done;
    }
    /* The immutable terminal lineage is the no-writers proof. The installation
     * lock additionally excludes a new generation during final verification;
     * lock availability/timeout alone never grants STOPPED. */
    if(snprintf(path,sizeof path,"%s/generations",ps.updater)>=(int)sizeof path)goto done;
    parent=checked_directory(path);if(parent<0)goto done;lifetime=openat(parent,".generation-lifetime.lock",O_RDWR|O_NOFOLLOW|O_CLOEXEC);struct stat st,named;
    if(lifetime<0||fstat(lifetime,&st)||!S_ISREG(st.st_mode)||st.st_uid!=geteuid()||st.st_nlink!=1||(st.st_mode&07777)!=0600||st.st_size)goto done;
    uint64_t until=millis()+2000;
    while(flock(lifetime,LOCK_EX|LOCK_NB)){if(errno!=EWOULDBLOCK||millis()>=until)goto done;struct timespec pause={0,10000000L};nanosleep(&pause,NULL);}
    if(fstatat(parent,".generation-lifetime.lock",&named,AT_SYMLINK_NOFOLLOW)||named.st_dev!=st.st_dev||named.st_ino!=st.st_ino||named.st_mode!=st.st_mode||named.st_nlink!=1||
       platform_install_apply(argv,platform_context_op(op),op_path,input,native,executor,held,statefd,shell,controller,1)||
       platform_stop_current_proof(op,op_path,argv[2],input,argv[4],native)||psc_retired(input)||migration_read(domain,"retirement.receipt",&retirement,0)||retirement.mode!=0600)goto done;
    char terminal_sha[65],host_sha[65],host_again[65];digest_bytes(snapshot,strlen(snapshot),terminal_sha);
    if(platform_host_retired(op,argv[2],input,native,&host,until,host_sha))goto done;
    int rn=snprintf(receipt,sizeof receipt,"BROray-platform-stop-current-receipt/2\nSTOPPED\n%s\n%s\n%s\n%s\n",sha,retirement.sha,terminal_sha,host_sha);
    if(rn<0||rn>=(int)sizeof receipt)goto done;digest_bytes(receipt,(size_t)rn,receipt_sha);
    int bn=snprintf(receipt_anchor,sizeof receipt_anchor,"BROray-platform-stop-current-anchor/1\n%s\n",receipt_sha);
    if(bn<0||bn>=(int)sizeof receipt_anchor||migration_record(op,"platform-stop-current.receipt-anchor",receipt_anchor,(size_t)bn,!complete)||
       migration_record(op,"platform-stop-current.receipt",receipt,(size_t)rn,!complete)||
       bg_record_exact(op,"platform-stop-current.intent",intent,size)||bg_record_exact(op,"platform-stop-current.intent-anchor",anchor,(size_t)an)||
       platform_stop_current_proof(op,op_path,argv[2],input,argv[4],native)||psc_retired(input)||
       platform_host_retired(op,argv[2],input,native,&host,0,host_again)||strcmp(host_sha,host_again)||
       bg_record_exact(domain,"retirement.receipt",retirement.bytes,retirement.size)||
       bg_record_exact(op,"platform-stop-current.receipt-anchor",receipt_anchor,(size_t)bn)||bg_record_exact(op,"platform-stop-current.receipt",receipt,(size_t)rn)||fsync(op))goto done;
    printf("{\"ok\":true,\"phase\":\"STOPPED\",\"generationId\":\"%s\",\"stopReceiptSha256\":\"%s\",\"replayed\":%s,\"allWritersStopped\":true,\"activationAllowed\":false}\n",ps.id,receipt_sha,complete?"true":"false");result=0;
done:
    free(intent);free(retirement.bytes);if(lifetime>=0)close(lifetime);if(parent>=0)close(parent);if(domain>=0)close(domain);if(host>=0)close(host);
    return result?migration_error("PLATFORM_STOP_CURRENT_UNCONFIRMED"):0;
}

/* Failure records are immutable evidence. Derive the only three allowed
 * historic boundaries from authenticated transaction bytes; never parse an
 * unknown record as authorization and never repair a partial pair. */
static int platform_retained_text(char record[2048],const struct bg_input *input,const char *migration,const char *native,const char *installed,const char *start,const char *stopped){
    const char *boundary=strcmp(stopped,"-")?"STOPPED":strcmp(start,"-")?"START_INTENT":"INSTALLED";
    return snprintf(record,2048,"{\"schemaVersion\":1,\"contract\":\"broray-platform-retained/1\",\"operationId\":\"%s\",\"stopNonce\":\"%s\",\"bootId\":\"%s\",\"migrationIntentSha256\":\"%s\",\"platformManifestSha256\":\"%s\",\"nativeSha256\":\"%s\",\"installedReceiptSha256\":\"%s\",\"startRecordSha256\":\"%s\",\"stopReceiptSha256\":\"%s\",\"boundary\":\"%s\",\"errorCode\":\"PLATFORM_RETAINED_RETRY_REQUIRED\",\"legacyReactivationAllowed\":false,\"activationAllowed\":false}\n",input->operation,input->nonce,boot,migration,input->manifest,native,installed,start,stopped,boundary);
}
static int platform_retained_proof(int op,const struct bg_input *input,const char *migration,const char *native){
    int present=psc_pair(op,"platform-retained.record","platform-retained.anchor");
    if(present<=0)return present;
    struct migration_file record,installed,start,stopped;memset(&record,0,sizeof record);memset(&installed,0,sizeof installed);memset(&start,0,sizeof start);memset(&stopped,0,sizeof stopped);
    char expected[2048],anchor[128],sha[65];int result=-1;
    if(migration_read(op,"platform-retained.record",&record,0)||record.mode!=0600||record.size>=sizeof expected||
       migration_read(platform_context_op(op),"platform-install/installed.receipt",&installed,0)||installed.mode!=0600||
       migration_read(op,"platform-start.record",&start,1)||(start.present&&start.mode!=0600)||
       migration_read(op,"platform-stop-current.receipt",&stopped,1)||(stopped.present&&stopped.mode!=0600))goto done;
    for(int boundary=0;boundary<3;boundary++){
        if((boundary>=1&&!start.present)||(boundary==2&&!stopped.present))continue;
        int n=platform_retained_text(expected,input,migration,native,installed.sha,boundary>=1?start.sha:"-",boundary==2?stopped.sha:"-");
        if(n<0||n>=(int)sizeof expected||record.size!=(size_t)n||memcmp(record.bytes,expected,(size_t)n))continue;
        digest_bytes(expected,(size_t)n,sha);int an=snprintf(anchor,sizeof anchor,"BROray-platform-retained-anchor/1\n%s\n",sha);
        if(an<0||an>=(int)sizeof anchor||bg_record_exact(op,"platform-retained.anchor",anchor,(size_t)an)||
           bg_record_exact(op,"platform-retained.record",expected,(size_t)n)||psc_pair(op,"platform-retained.record","platform-retained.anchor")!=1)goto done;
        result=0;break;
    }
done:
    free(record.bytes);free(installed.bytes);free(start.bytes);free(stopped.bytes);return result;
}

/* First migration keeps the installed supervised platform on failure. This
 * entry records a verified safe boundary, never downgrades platform files or
 * releases the fence. A retained receipt is evidence, not start/commit authority. */
static int platform_preserve_apply(char **argv,int op,const char *op_path,const struct bg_input *input,const char *native,const struct identity *executor,int held,int statefd,const char *shell,const char *controller){
    int generation_lock=-1,result=75;char record[2048],anchor[128],sha[65];
    struct migration_file installed,start,stopped;memset(&installed,0,sizeof installed);memset(&start,0,sizeof start);memset(&stopped,0,sizeof stopped);
    if(platform_rollback_generation_guard(op,op_path,argv[2],input,argv[4],native,&generation_lock,1)||
       platform_install_apply(argv,platform_context_op(op),op_path,input,native,executor,held,statefd,shell,controller,1)||
       migration_read(platform_context_op(op),"platform-install/installed.receipt",&installed,0)||installed.mode!=0600||
       migration_read(op,"platform-start.record",&start,1)||
       migration_read(op,"platform-stop-current.receipt",&stopped,1))goto done;
    if((start.present&&(start.mode!=0600||platform_start_intent_apply(argv,op,op_path,input,native,executor,held,statefd,shell,controller,1)))||
       (stopped.present&&(!start.present||stopped.mode!=0600)))goto done;
    const char *boundary=stopped.present?"STOPPED":start.present?"START_INTENT":"INSTALLED";
    int n=platform_retained_text(record,input,argv[4],native,installed.sha,start.present?start.sha:"-",stopped.present?stopped.sha:"-");
    if(n<0||n>=(int)sizeof record)goto done;digest_bytes(record,(size_t)n,sha);
    int an=snprintf(anchor,sizeof anchor,"BROray-platform-retained-anchor/1\n%s\n",sha);
    int present=psc_pair(op,"platform-retained.record","platform-retained.anchor");
    if(an<0||an>=(int)sizeof anchor||present<0||
       migration_record(op,"platform-retained.anchor",anchor,(size_t)an,!present)||
       migration_record(op,"platform-retained.record",record,(size_t)n,!present)||
       platform_rollback_generation_guard(op,op_path,argv[2],input,argv[4],native,&generation_lock,0)||
       platform_install_apply(argv,platform_context_op(op),op_path,input,native,executor,held,statefd,shell,controller,1)||
       bg_record_exact(platform_context_op(op),"platform-install/installed.receipt",installed.bytes,installed.size)||
       (start.present?bg_record_exact(op,"platform-start.record",start.bytes,start.size):bg_exists(op,"platform-start.record")!=0)||
       (stopped.present?bg_record_exact(op,"platform-stop-current.receipt",stopped.bytes,stopped.size):bg_exists(op,"platform-stop-current.receipt")!=0)||
       bg_record_exact(op,"platform-retained.anchor",anchor,(size_t)an)||bg_record_exact(op,"platform-retained.record",record,(size_t)n)||fsync(op))goto done;
    printf("{\"ok\":false,\"phase\":\"PLATFORM_RETAINED\",\"platformRetained\":true,\"boundary\":\"%s\",\"errorCode\":\"PLATFORM_RETAINED_RETRY_REQUIRED\",\"receiptSha256\":\"%s\",\"replayed\":%s,\"activationAllowed\":false}\n",boundary,sha,present?"true":"false");result=0;
done:
    free(installed.bytes);free(start.bytes);free(stopped.bytes);if(generation_lock>=0)close(generation_lock);
    return migration_error(result?"PLATFORM_RETENTION_UNCONFIRMED":"PLATFORM_RETAINED_RETRY_REQUIRED");
}

/* Prepare a distinct successor without launching or erasing the retired
 * generation. The exact previous STOPPED receipt is the retry idempotency key.
 * Launch integration is separate: this receipt alone grants no readiness. */
static int platform_retry_prepare_apply(char **argv,int op,const char *op_path,const struct bg_input *input,const char *native,const struct identity *executor,int held,int statefd,const char *shell,const char *controller){
    int generation_lock=-1,parent=-1,attempt=-1,result=75,replay=0,previous=-1;
    struct migration_file installed,stopped,retained,retry;memset(&installed,0,sizeof installed);memset(&stopped,0,sizeof stopped);memset(&retained,0,sizeof retained);memset(&retry,0,sizeof retry);
    char seed_sha[65],id[65],record[2048],sha[65],anchor[128],parent_path[PATH_MAX],attempt_path[PATH_MAX];
    if(platform_attempt_load(platform_context_op(op),op_path,argv[2],input,argv[4],native))goto done;
    for(int i=0;i<=pa.count;i++){
        struct migration_file receipt;memset(&receipt,0,sizeof receipt);
        if(migration_read(pa.node[i].fd,"platform-stop-current.receipt",&receipt,1)){free(receipt.bytes);goto done;}
        if(receipt.present&&!strcmp(receipt.sha,argv[6])){if(previous>=0){free(receipt.bytes);goto done;}previous=i;}
        free(receipt.bytes);
    }
    if(previous<0)goto done;op=pa.node[previous].fd;
    if(previous<pa.count){
        /* A delayed old retry cannot advance a later attempt. The immediate
         * successor replay is read-only, including while it is already READY. */
        if(previous+1!=pa.count||migration_read(op,"platform-retry.record",&retry,0)||retry.mode!=0600)goto done;
        strcpy(id,pa.node[previous+1].id);strcpy(sha,retry.sha);replay=1;goto verified;
    }
    if(pa.count+1>=PLATFORM_ATTEMPT_LIMIT||
       platform_rollback_generation_guard(op,op_path,argv[2],input,argv[4],native,&generation_lock,1)||
       platform_install_apply(argv,platform_context_op(op),op_path,input,native,executor,held,statefd,shell,controller,1)||
       platform_stopped_current_proof(op,op_path,argv[2],input,argv[4],native)||
       platform_retained_proof(op,input,argv[4],native)||psc_pair(op,"platform-retained.record","platform-retained.anchor")!=1||
       migration_read(platform_context_op(op),"platform-install/installed.receipt",&installed,0)||installed.mode!=0600||
       migration_read(op,"platform-stop-current.receipt",&stopped,0)||stopped.mode!=0600||strcmp(stopped.sha,argv[6])||
       migration_read(op,"platform-retained.record",&retained,0)||retained.mode!=0600)goto done;
    int n=platform_retry_text(record,seed_sha,id,input,argv[4],native,installed.sha,ps.id,ps.launch,ps.transaction,stopped.sha,retained.sha);
    if(n<0||n>=(int)sizeof record)goto done;digest_bytes(record,(size_t)n,sha);
    int an=snprintf(anchor,sizeof anchor,"BROray-platform-retry-anchor/1\n%s\n",sha);
    int present=psc_pair(op,"platform-retry.record","platform-retry.anchor"),directory=bg_exists(platform_context_op(op),"platform-attempts");
    if(an<0||an>=(int)sizeof anchor||present!=0||directory<0||directory!=(pa.count>0)||
       snprintf(parent_path,sizeof parent_path,"%s/platform-attempts",op_path)>=(int)sizeof parent_path||
       snprintf(attempt_path,sizeof attempt_path,"%s/%s",parent_path,id)>=(int)sizeof attempt_path)goto done;
    for(int i=0;i<=pa.count;i++)if(!strcmp(pa.node[i].id,id))goto done;
    if(directory){parent=checked_directory(parent_path);if(parent<0||bg_exists(parent,id)!=0)goto done;}
    if(migration_record(op,"platform-retry.anchor",anchor,(size_t)an,1)||migration_record(op,"platform-retry.record",record,(size_t)n,1))goto done;
    if(!directory&&(mkdirat(platform_context_op(op),"platform-attempts",0700)||fsync(platform_context_op(op))))goto done;
    if(parent<0)parent=checked_directory(parent_path);
    if(parent<0||(!directory&&bg_empty(parent))||mkdirat(parent,id,0700)||fsync(parent))goto done;
    attempt=checked_directory(attempt_path);if(attempt<0||bg_empty(attempt)||migration_record(attempt,"attempt.record",record,(size_t)n,1)||
       migration_sync_directory(attempt_path,attempt)||migration_sync_directory(parent_path,parent)||
       platform_attempt_load(platform_context_op(op),op_path,argv[2],input,argv[4],native)||
       platform_rollback_generation_guard(op,op_path,argv[2],input,argv[4],native,&generation_lock,0)||
       platform_install_apply(argv,platform_context_op(op),op_path,input,native,executor,held,statefd,shell,controller,1)||
       platform_retained_proof(op,input,argv[4],native)||bg_record_exact(op,"platform-retained.record",retained.bytes,retained.size)||
       bg_record_exact(op,"platform-stop-current.receipt",stopped.bytes,stopped.size)||
       bg_record_exact(platform_context_op(op),"platform-install/installed.receipt",installed.bytes,installed.size)||
       bg_record_exact(op,"platform-retry.record",record,(size_t)n)||bg_record_exact(op,"platform-retry.anchor",anchor,(size_t)an)||
       bg_record_exact(attempt,"attempt.record",record,(size_t)n)||psc_pair(op,"platform-retry.record","platform-retry.anchor")!=1)goto done;
verified:
    printf("{\"ok\":true,\"phase\":\"RETRY_PREPARED\",\"generationId\":\"%s\",\"previousStopReceiptSha256\":\"%s\",\"retryReceiptSha256\":\"%s\",\"replayed\":%s,\"serviceStarted\":false,\"activationAllowed\":false}\n",id,argv[6],sha,replay?"true":"false");result=0;
done:
    free(installed.bytes);free(stopped.bytes);free(retained.bytes);free(retry.bytes);if(attempt>=0)close(attempt);if(parent>=0)close(parent);if(generation_lock>=0)close(generation_lock);
    return result?migration_error("PLATFORM_RETRY_UNCONFIRMED"):0;
}

/* Independent append-only witness of every durable platform-generation
 * revision. The ledger's own immutable publication and containment are not
 * changed. A later kernel boot cannot accept a rolled-back valid prefix when
 * these independently published revision hashes still witness its tail. */
static int platform_ledger_witness(unsigned long nr,const char *bytes,size_t size){
    if(!pl.enabled)return 0;
    int present=bg_exists(pl.startfd,"ledger-witnesses");if(present<0)return -1;
    if(!present){if(nr!=1||mkdirat(pl.startfd,"ledger-witnesses",0700)||fsync(pl.startfd))return -1;}
    char path[PATH_MAX],name[64],digest[65],text[384];
    if(snprintf(path,sizeof path,"%s/ledger-witnesses",pl.startdir)>=(int)sizeof path)return -1;
    int fd=checked_directory(path);if(fd<0)return -1;
    if(nr==1&&bg_empty(fd)){close(fd);return -1;}
    record_name(nr,name);digest_bytes(bytes,size,digest);
    int n=snprintf(text,sizeof text,"BROray-platform-ledger-witness/1\n%s\n%s\n%s\n%lu\n%s\n",pl.id,pl.manifest,boot,nr,digest);
    int bad=n<0||n>=(int)sizeof text||migration_record(fd,name,text,(size_t)n,1);close(fd);return bad?-1:0;
}
