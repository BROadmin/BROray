/* Independent init-owned application-service launcher. It never adopts or
 * detaches an updater descendant. Only an authenticated, live generation child
 * may ask it to execute a fixed app init action. Boot integration is separate.
 */
#define SERVICE_REQUEST_LIMIT 1024
struct service_receipt {char id[65],intent_name[96],done_name[96],semantic[512],done[256];char *intent;size_t intent_size;struct service_receipt *next;};
static struct service_receipt *service_receipts;
static unsigned service_count;
static char service_terminal[512];
static int service_error(const char *reason){fprintf(stderr,"SERVICE_FIRST_ERROR=%s\n",reason);return 75;}
/* Empty exit 75 is reserved by operation-client for an unexecuted guard wait.
 * A protected replacement refusal is final, including after durable writes. */
static int service_replacement_error(const char *reason){
    printf("{\"ok\":false,\"errorCode\":\"%s\"}\n",reason);return service_error(reason);
}
static int service_scope_valid(int argc,char **argv,int client){
    return argc==(client?14:9)&&migration_path(argv[2])&&migration_path(argv[3])&&token(argv[4],64)&&hex64(argv[5])&&migration_path(argv[6])&&migration_path(argv[7])&&hex64(argv[8]);
}
static int service_untraced(void){
    char b[8192];ssize_t n=read_file("/proc/self/status",b,sizeof b-1);if(n<=0)return 0;b[n]=0;
    char *p=strstr(b,"\nTracerPid:\t");return p&&!strncmp(p,"\nTracerPid:\t0\n",14);
}
static void service_close_fds(int first,int second){
    DIR *d=opendir("/proc/self/fd");if(!d)_exit(74);int own=dirfd(d);struct dirent *e;
    while((e=readdir(d))){char *end;long fd=strtol(e->d_name,&end,10);if(*end||fd<3||fd==own||fd==first||fd==second)continue;close((int)fd);}closedir(d);
}
static int service_allowed(const char *action,const char *name){
    if(strcmp(action,"start")&&strcmp(action,"stop")&&strcmp(action,"status"))return 0;
    const char *names[]={"S23broray-monitor","S24broray","S25broray-web","S27broray-auto-switch","S28broray-subscriptions"};
    for(unsigned i=0;i<sizeof names/sizeof names[0];i++)if(!strcmp(name,names[i]))return 1;return 0;
}
static int service_slot(const char *s){
    if(!s[0]||strlen(s)>96||!strcmp(s,".")||!strcmp(s,".."))return 0;
    for(;*s;s++)if(!((*s>='a'&&*s<='z')||(*s>='A'&&*s<='Z')||(*s>='0'&&*s<='9')||*s=='-'||*s=='_'||*s=='.'))return 0;return 1;
}
static int service_peer(int fd,char **argv,struct identity *id){
    struct ucred peer;socklen_t size=sizeof peer;char own[65],remote[65],path[64],cmd[65536];struct identity again;
    if(getsockopt(fd,SOL_SOCKET,SO_PEERCRED,&peer,&size)||size!=sizeof peer||peer.pid<=1||peer.uid!=geteuid()||capture(peer.pid,id))return -1;
    if(peer_executable_hash(getpid(),own)||peer_executable_hash(peer.pid,remote)||strcmp(own,remote))return -1;
    snprintf(path,sizeof path,"/proc/%d/cmdline",peer.pid);ssize_t n=read_file(path,cmd,sizeof cmd);if(n<=0||cmd[n-1])return -1;
    char sha[65];digest_bytes(cmd,(size_t)n,sha);if(strcmp(sha,id->cmd))return -1;size_t at=0;
    for(int i=0;i<9;i++){if(at>=(size_t)n||!cmd[at])return -1;const char *expected=i==1?"service-host":argv[i];if(i&&strcmp(cmd+at,expected))return -1;at+=strlen(cmd+at)+1;}
    return at!=(size_t)n||capture(peer.pid,&again)||!identity_equal(id,&again)?-1:0;
}
static int service_host_record_sha(char **argv,const struct identity *owner,char sha[65]);
static int service_actor(int fd,char **argv,struct identity *actor){
    struct ucred peer;socklen_t size=sizeof peer;struct identity again;char self_sha[65],actor_sha[65];
    if(getsockopt(fd,SOL_SOCKET,SO_PEERCRED,&peer,&size)||size!=sizeof peer||peer.uid!=geteuid()||peer.pid<=1||capture(peer.pid,actor)){service_error("ACTOR_IDENTITY_UNCONFIRMED");return -1;}
    if(peer_executable_hash(getpid(),self_sha)||peer_executable_hash(peer.pid,actor_sha)||strcmp(self_sha,actor_sha)){service_error("ACTOR_NATIVE_BYTES_UNCONFIRMED");return -1;}
    char *request[]={argv[0],"control",argv[3],"STATUS",argv[4],argv[5],"service-auth","service-auth"};
    if(control_exchange(8,request,0)||!strstr(snapshot,"\"state\":\"RUNNING\",\"supervisedFromBirth\":true,")){service_error("ACTOR_GENERATION_NOT_RUNNING");return -1;}
    /* Bind the receiving host at the existing authenticated authorization
     * boundary, before any journal write or init action. A second STATUS
     * round trip in the traced client adds no authority and delays replies. */
    if(!strstr(snapshot,"\"platformLaunch\":null")){
        char sha[65],field[128];
        if(service_host_record_sha(argv,&supervisor,sha))return -1;
        snprintf(field,sizeof field,"\"serviceHostRecordSha256\":\"%s\"",sha);
        if(!strstr(snapshot,"\"platformReady\":true,")||!strstr(snapshot,field)){service_error("HOST_GENERATION_BINDING_UNCONFIRMED");return -1;}
    }
    char *begin=strstr(snapshot,"\"children\":["),*end=strstr(snapshot,"],\"awaitingBirth\":[");if(!begin||!end||end<=begin)return -1;
    char *identity=NULL;size_t identity_size=0;FILE *f=open_memstream(&identity,&identity_size);if(!f)return -1;identity_json(f,actor);if(fclose(f)){free(identity);return -1;}
    char *found=strstr(begin,identity);int bad=!found||found+identity_size>end;free(identity);
    if(bad||capture(peer.pid,&again)||!identity_equal(actor,&again)){service_error("ACTOR_NOT_IN_EXACT_CHILD_LEDGER");return -1;}return 0;
}
static int service_records_valid(int base,const char *host_record,size_t host_size){
    if(migration_record(base,"host.record",host_record,host_size,0))return -1;
    if(service_terminal[0]&&migration_record(base,"retirement.receipt",service_terminal,strlen(service_terminal),0))return -1;
    for(struct service_receipt *r=service_receipts;r;r=r->next){
        if(migration_record(base,r->intent_name,r->intent,r->intent_size,0))return -1;
        if(r->done[0]&&migration_record(base,r->done_name,r->done,strlen(r->done),0))return -1;
    }
    DIR *d=directory_stream(base);if(!d)return -1;struct dirent *e;int bad=0;errno=0;
    while((e=readdir(d))){const char *name=e->d_name;if(!strcmp(name,".")||!strcmp(name,"..")||!strcmp(name,"host.record")||!strcmp(name,"control"))continue;
        if(service_terminal[0]&&!strcmp(name,"retirement.receipt"))continue;
        int known=0;for(struct service_receipt *r=service_receipts;r;r=r->next)if(!strcmp(name,r->intent_name)||(r->done[0]&&!strcmp(name,r->done_name))){known=1;break;}
        if(!known){bad=1;break;}errno=0;
    }if(!e&&errno)bad=1;closedir(d);return bad?-1:0;
}
/* Terminal evidence covers every completed request, in stable name order.
 * Unknown entries, unmatched intents/results and pending publication refuse
 * retirement. No evidence is removed or reconstructed. */
static int service_name_compare(const void *a,const void *b){return strcmp(a,b);}
static int service_journal_digest(int base,char hash[65]){
    char (*names)[96]=calloc(SERVICE_REQUEST_LIMIT*2,sizeof *names);if(!names)return -1;
    DIR *d=directory_stream(base);if(!d){free(names);return -1;}struct dirent *e;unsigned count=0;int bad=0;errno=0;
    while((e=readdir(d))){const char *s=e->d_name;
        if(!strcmp(s,".")||!strcmp(s,"..")||!strcmp(s,"host.record")||!strcmp(s,"control")||!strcmp(s,"retirement.receipt"))continue;
        size_t len=strlen(s);const char *dot=strrchr(s,'.');char id[65],pair[96];
        if(strncmp(s,"request-",8)||!dot||(strcmp(dot,".intent")&&strcmp(dot,".done"))||dot-s<=8||dot-s>72||len>=96||count==SERVICE_REQUEST_LIMIT*2){bad=1;break;}
        size_t size=(size_t)(dot-s)-8;memcpy(id,s+8,size);id[size]=0;
        if(!token(id,64)){bad=1;break;}
        snprintf(pair,sizeof pair,"request-%s%s",id,!strcmp(dot,".intent")?".done":".intent");struct stat st;
        if(fstatat(base,pair,&st,AT_SYMLINK_NOFOLLOW)||!S_ISREG(st.st_mode)||st.st_uid!=geteuid()||st.st_nlink!=1||(st.st_mode&07777)!=0600){bad=1;break;}
        strcpy(names[count++],s);errno=0;
    }
    if(!e&&errno)bad=1;closedir(d);if(bad){free(names);return -1;}
    qsort(names,count,sizeof *names,service_name_compare);struct gen_sha digest;gen_sha_init(&digest);
    for(unsigned i=0;i<count;i++){char *bytes=NULL,sha[65];size_t size=0;
        if(safe_bytes_at(base,names[i],&bytes,&size)){free(names);return -1;}
        digest_bytes(bytes,size,sha);free(bytes);gen_sha_add(&digest,names[i],strlen(names[i])+1);gen_sha_add(&digest,sha,64);
    }
    gen_sha_end(&digest,hash);free(names);return 0;
}
/* Return one while no canonical retirement exists, zero for exact terminal
 * evidence, negative for unknown/corrupt evidence. Used by the original host
 * and by the protected controller; a timeout never supplies this proof. */
static int service_retirement_text_at(int base,char **argv,const char *host,size_t host_size,char text[512],const char *historical_boot){
    int domain=checked_directory(argv[3]);if(domain<0)return errno==ENOENT?1:-1;
    struct stat st;if(fstatat(domain,"retirement.receipt",&st,AT_SYMLINK_NOFOLLOW)){int absent=errno==ENOENT;close(domain);return absent?1:-1;}
    /* The receipt can already be linked while its native publisher is still
     * completing pending unlink/fsync. Wait for its existing lifetime lock
     * before consuming terminal history. The lock alone never grants proof;
     * complete exact ledger validation below remains mandatory. */
    int lifetime=openat(domain,"lifetime.lock",O_RDONLY|O_NOFOLLOW|O_CLOEXEC);struct stat held,named;
    if(lifetime<0||fstat(lifetime,&held)||!S_ISREG(held.st_mode)||held.st_uid!=geteuid()||held.st_nlink!=1||(held.st_mode&07777)!=0600||held.st_size||
       fstatat(domain,"lifetime.lock",&named,AT_SYMLINK_NOFOLLOW)||named.st_dev!=held.st_dev||named.st_ino!=held.st_ino||named.st_mode!=held.st_mode||named.st_uid!=held.st_uid||named.st_nlink!=1||named.st_size){if(lifetime>=0)close(lifetime);close(domain);return -1;}
    if(flock(lifetime,LOCK_EX|LOCK_NB)){int busy=errno==EWOULDBLOCK;close(lifetime);close(domain);return busy?1:-1;}
    struct retired_record retired;char name[64],*last=NULL,*receipt=NULL,host_sha[65],retired_sha[65],journal[65],field[160];size_t size=0,receipt_size=0;int result=-1;
    if(retirement_valid(domain,argv[3],&retired)||strcmp(retired.gen,argv[4])||strcmp(retired.sha,argv[5]))goto done;
    record_name(retired.total,name);if(safe_bytes_at(domain,name,&last,&size)||memchr(last,0,size))goto done;
    if(strstr(last,"\"platformLaunch\":null")){result=1;goto done;}
    digest_bytes(host,host_size,host_sha);snprintf(field,sizeof field,"\"serviceHostRecordSha256\":\"%s\"",host_sha);
    if(!strstr(last,field))goto done;snprintf(field,sizeof field,"\"bootId\":\"%s\"",historical_boot);
    if(!strstr(last,field)||!strstr(last,"\"supervisedFromBirth\":true,")||
       migration_record(base,"host.record",host,host_size,0)||service_journal_digest(base,journal)||
       safe_bytes_at(domain,"retirement.receipt",&receipt,&receipt_size))goto done;
    digest_bytes(receipt,receipt_size,retired_sha);
    int n=snprintf(text,512,"BROray-app-service-retired/1\n%s\n%s\n%s\n%s\n%s\n%s\n",argv[4],historical_boot,host_sha,retired_sha,retired.last,journal);
    if(n<0||n>=512||retirement_valid(domain,argv[3],NULL)||
       fstatat(domain,"lifetime.lock",&named,AT_SYMLINK_NOFOLLOW)||named.st_dev!=held.st_dev||named.st_ino!=held.st_ino||named.st_mode!=held.st_mode||named.st_uid!=held.st_uid||named.st_nlink!=1||named.st_size)goto done;
    result=0;
done:free(last);free(receipt);close(lifetime);close(domain);return result;
}
static int service_retirement_text(int base,char **argv,const char *host,size_t host_size,char text[512]){return service_retirement_text_at(base,argv,host,host_size,text,boot);}
static int service_script(int root,const char *name,const char *sha,const char *slot,struct migration_file *script){
    struct migration_file marker,manifest_file;memset(&marker,0,sizeof marker);memset(&manifest_file,0,sizeof manifest_file);int result=-1;char relative[128],expected[128],row[256];
    snprintf(relative,sizeof relative,"opt/broray/current/init/%s",name);snprintf(expected,sizeof expected,"%s\n",slot);
    if(migration_read(root,"opt/broray/current/.broray-slot",&marker,0)||marker.size!=strlen(expected)||memcmp(marker.bytes,expected,marker.size))goto done;
    if(migration_read(root,"opt/broray/current/SHA256SUMS",&manifest_file,0)||migration_read(root,relative,script,0)||script->mode!=0755||strcmp(script->sha,sha))goto done;
    int length=snprintf(row,sizeof row,"%s  init/%s\n",sha,name),matches=0;size_t offset=0;
    while(offset<manifest_file.size){char *p=manifest_file.bytes+offset,*nl=memchr(p,'\n',manifest_file.size-offset);if(!nl)goto done;
        char suffix[96];snprintf(suffix,sizeof suffix,"  init/%s",name);size_t size=(size_t)(nl-p);
        if(size>=strlen(suffix)&&!memcmp(nl-strlen(suffix),suffix,strlen(suffix))){if(size+1!=(size_t)length||memcmp(p,row,(size_t)length))goto done;matches++;}
        offset=(size_t)(nl-manifest_file.bytes)+1;
    }
    if(matches==1)result=0;
done:free(marker.bytes);free(manifest_file.bytes);return result;
}
static int service_execute(int ashfd,const struct migration_file *script,const char *action){
    int p[2];if(pipe2(p,O_CLOEXEC))return -1;pid_t child=fork();if(child<0){close(p[0]);close(p[1]);return -1;}
    if(!child){
        close(p[1]);service_close_fds(p[0],ashfd);if(fcntl(p[0],F_SETFD,0)<0)_exit(74);
        int null=open("/dev/null",O_RDONLY|O_CLOEXEC);if(null<0||dup2(null,0)<0)_exit(74);if(null>2)close(null);
        /* Script comes from verified memory, not a mutable pathname. It has
         * no inherited coordinator/generation descriptors or environment. */
        if(clearenv()||setenv("PATH","/opt/bin:/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin",1)||setenv("LC_ALL","C",1))_exit(74);
        char path[64];snprintf(path,sizeof path,"/proc/self/fd/%d",p[0]);char *args[]={"ash",path,(char*)action,NULL};extern char **environ;
        fexecve(ashfd,args,environ);_exit(127);
    }
    close(p[0]);size_t at=0;int failed=0;while(at<script->size){ssize_t n=write(p[1],script->bytes+at,script->size-at);if(n<0&&errno==EINTR)continue;if(n<=0){failed=1;break;}at+=(size_t)n;}close(p[1]);
    int status;pid_t got;do{got=waitpid(child,&status,0);}while(got<0&&errno==EINTR);
    if(failed||got!=child||!WIFEXITED(status))return -1;return WEXITSTATUS(status);
}
static int service_interpreter_file_valid(const struct stat *st){
    /* Entware ships root-owned BusyBox as 4755. Executing it is not a
     * privilege transition only when BOTH real and effective UID are root.
     * Setgid/sticky bits, writable files and additional links remain invalid. */
    return S_ISREG(st->st_mode)&&st->st_uid==geteuid()&&st->st_nlink==1&&
        !(st->st_mode&03022)&&(st->st_mode&0100)&&
        (!(st->st_mode&S_ISUID)||(st->st_uid==0&&getuid()==0&&geteuid()==0));
}
static int service_interpreter_valid(int held,const char *path,const char *expected){
    int fs=migration_directory("/");if(fs<0)return -1;int named=migration_relative(fs,path+1);close(fs);if(named<0)return -1;
    struct stat a,b,after;char sha[65];int bad=fstat(held,&a)||fstat(named,&b)||a.st_dev!=b.st_dev||a.st_ino!=b.st_ino||a.st_mode!=b.st_mode||!service_interpreter_file_valid(&a)||lseek(held,0,SEEK_SET)!=0||hash_fd(held,sha)||strcmp(sha,expected)||fstat(held,&after)||!service_interpreter_file_valid(&after)||after.st_uid!=a.st_uid||after.st_mode!=a.st_mode||after.st_size!=a.st_size;
    close(named);return bad?-1:0;
}
static int service_request(int client,int base,int root,int ashfd,char **argv,const char *host_record,size_t host_size){
    /* Client verifies this server's executable and exact command scope before
     * sending a mutating request. Under ptrace that verified handshake exceeded
     * the old 100 ms limit (saved CP46). Use the existing generation-control
     * two-second I/O bound; expiration still performs no action or cleanup.
     * This does not change any test deadline or supply ownership evidence. */
    char body[512],action[16],name[64],id[65],sha[65],slot[97],extra;struct pollfd p={client,POLLIN,0};if(poll(&p,1,2000)!=1){service_error("REQUEST_MESSAGE_NOT_READY");return -1;}
    ssize_t n=recv(client,body,sizeof body-1,MSG_TRUNC);if(n<=0||(size_t)n>=sizeof body-1)return -1;body[n]=0;
    if(memchr(body,0,(size_t)n))return -1;
    /* Read-only lifetime proof is also available before the updater is born.
     * It authorizes no app action and cannot repair/recreate host evidence. */
    if(n==6&&!memcmp(body,"STATUS",6)){
        if(service_records_valid(base,host_record,host_size)||service_interpreter_valid(ashfd,argv[7],argv[8]))return -2;
        return send(client,host_record,host_size,MSG_NOSIGNAL)==(ssize_t)host_size?0:-1;
    }
    struct identity actor;if(service_actor(client,argv,&actor))return -1;
    if(sscanf(body,"%15s %63s %64s %64s %96s %c",action,name,id,sha,slot,&extra)!=5||!service_allowed(action,name)||!token(id,64)||!hex64(sha)||!service_slot(slot))return -1;
    char canonical[512];int length=snprintf(canonical,sizeof canonical,"%s %s %s %s %s",action,name,id,sha,slot);if(n!=length||memcmp(body,canonical,(size_t)n))return -1;
    if(service_records_valid(base,host_record,host_size))return -2;
    struct service_receipt *r;for(r=service_receipts;r;r=r->next)if(!strcmp(r->id,id))break;
    if(r){if(strcmp(r->semantic,canonical)||!r->done[0])return -1;return send(client,r->done,strlen(r->done),MSG_NOSIGNAL)==(ssize_t)strlen(r->done)?0:-1;}
    if(service_interpreter_valid(ashfd,argv[7],argv[8])){service_error("ASH_BYTES_CHANGED");return -2;}
    struct migration_file script;memset(&script,0,sizeof script);
    if(service_count>=SERVICE_REQUEST_LIMIT||service_script(root,name,sha,slot,&script)){free(script.bytes);return -1;}
    struct identity again;if(service_actor(client,argv,&again)||!identity_equal(&actor,&again)){free(script.bytes);return -1;}
    r=calloc(1,sizeof *r);if(!r){free(script.bytes);return -2;}strcpy(r->id,id);strcpy(r->semantic,canonical);snprintf(r->intent_name,sizeof r->intent_name,"request-%s.intent",id);snprintf(r->done_name,sizeof r->done_name,"request-%s.done",id);
    FILE *f=open_memstream(&r->intent,&r->intent_size);if(!f){free(r);free(script.bytes);return -2;}
    fprintf(f,"BROray-app-service-intent/1\n%s\n%s\n%s\n%s\n",argv[4],argv[5],boot,canonical);identity_json(f,&actor);fputc('\n',f);
    if(fclose(f)||migration_record(base,r->intent_name,r->intent,r->intent_size,1)){free(r->intent);free(r);free(script.bytes);return -2;}
    r->next=service_receipts;service_receipts=r;service_count++;
    if(service_records_valid(base,host_record,host_size)||service_interpreter_valid(ashfd,argv[7],argv[8])){free(script.bytes);return -2;}
    int rc=service_execute(ashfd,&script,action);free(script.bytes);if(rc<0)return -2;
    char request_sha[65];digest_bytes(canonical,(size_t)length,request_sha);snprintf(r->done,sizeof r->done,"{\"ok\":%s,\"requestId\":\"%s\",\"requestSha256\":\"%s\",\"exitCode\":%d}\n",rc==0?"true":"false",id,request_sha,rc);
    if(migration_record(base,r->done_name,r->done,strlen(r->done),1)||service_records_valid(base,host_record,host_size))return -2;
    return send(client,r->done,strlen(r->done),MSG_NOSIGNAL)==(ssize_t)strlen(r->done)?0:-1;
}
static int service_host(int argc,char **argv){
    if(!service_scope_valid(argc,argv,0))return 64;
    if(!service_untraced()||getenv("BRORAY_UPDATER_GENERATION"))return service_error("INDEPENDENT_INIT_ORIGIN_REQUIRED");
    service_close_fds(-1,-1);umask(077);signal(SIGPIPE,SIG_IGN);
    int base=checked_directory(argv[2]);if(base<0||flock(base,LOCK_EX|LOCK_NB)||empty_directory(base)!=1)return service_error("LAUNCHER_DOMAIN_NOT_EMPTY_OR_OWNED");
    int root=migration_directory(argv[6]),fs=migration_directory("/");if(root<0||fs<0)return service_error("LAUNCHER_ROOT_UNCONFIRMED");
    int ashfd=migration_relative(fs,argv[7]+1);close(fs);char sha[65];struct stat ast;
    if(ashfd<0||fstat(ashfd,&ast)||!service_interpreter_file_valid(&ast)||hash_fd(ashfd,sha)||strcmp(sha,argv[8]))return service_error("ASH_BYTES_UNCONFIRMED");
    if(migration_boot(boot)||capture(getpid(),&supervisor)||migration_sync_directory(argv[2],base))return service_error("LAUNCHER_IDENTITY_UNCONFIRMED");
    char *host_record=NULL;size_t host_size=0;FILE *f=open_memstream(&host_record,&host_size);if(!f)return 74;
    fprintf(f,"BROray-independent-app-service/1\n");for(int i=2;i<9;i++)fprintf(f,"%s\n",argv[i]);identity_json(f,&supervisor);fputc('\n',f);
    if(fclose(f)||migration_record(base,"host.record",host_record,host_size,1))return service_error("LAUNCHER_INTENT_NOT_DURABLE");
    int server=make_socket(argv[2]);if(server<0||fsync(base))return service_error("LAUNCHER_SOCKET_UNAVAILABLE");
    for(;;){
        if(service_records_valid(base,host_record,host_size))return service_error("LAUNCHER_EVIDENCE_CHANGED");
        char terminal[512];int retired=service_retirement_text(base,argv,host_record,host_size,terminal);
        if(retired<0)return service_error("LAUNCHER_RETIREMENT_UNCONFIRMED");
        if(!retired){
            for(struct service_receipt *r=service_receipts;r;r=r->next)if(!r->done[0])return service_error("LAUNCHER_REQUEST_INCOMPLETE");
            if(service_records_valid(base,host_record,host_size)||migration_record(base,"retirement.receipt",terminal,strlen(terminal),1))return service_error("LAUNCHER_RETIREMENT_NOT_DURABLE");
            strcpy(service_terminal,terminal);
            if(service_records_valid(base,host_record,host_size)||service_retirement_text(base,argv,host_record,host_size,terminal)||strcmp(terminal,service_terminal))return service_error("LAUNCHER_RETIREMENT_CHANGED");
            return 0;
        }
        struct pollfd p={server,POLLIN,0};int ready=poll(&p,1,1000);if(ready<0&&errno==EINTR)continue;if(ready<0)return 74;if(!ready)continue;
        int client=accept4(server,NULL,NULL,SOCK_CLOEXEC);if(client<0){if(errno==EAGAIN||errno==EINTR)continue;return 74;}
        int result=service_request(client,base,root,ashfd,argv,host_record,host_size);close(client);if(result==-2)return service_error("LAUNCHER_PUBLICATION_UNCONFIRMED");
    }
}
static int service_host_record_sha(char **argv,const struct identity *owner,char sha[65]){
    if(migration_boot(boot))return -1;
    char *text=NULL;size_t size=0;FILE *f=open_memstream(&text,&size);if(!f)return -1;
    fputs("BROray-independent-app-service/1\n",f);for(int i=2;i<9;i++)fprintf(f,"%s\n",argv[i]);identity_json(f,owner);fputc('\n',f);
    int bad=fclose(f);if(!bad)digest_bytes(text,size,sha);free(text);return bad?-1:0;
}
static int service_client(int argc,char **argv){
    if(!service_scope_valid(argc,argv,1)||!service_allowed(argv[9],argv[10])||!token(argv[11],64)||!hex64(argv[12])||!service_slot(argv[13]))return 64;
    int base=checked_directory(argv[2]);struct stat st;if(base<0)return 75;int bad=fstatat(base,"control",&st,AT_SYMLINK_NOFOLLOW)||!S_ISSOCK(st.st_mode)||st.st_uid!=geteuid()||st.st_nlink!=1||(st.st_mode&07777)!=0700;close(base);if(bad)return 75;
    struct sockaddr_un addr;memset(&addr,0,sizeof addr);addr.sun_family=AF_UNIX;if(snprintf(addr.sun_path,sizeof addr.sun_path,"%s/control",argv[2])>=(int)sizeof addr.sun_path)return 64;
    int fd=socket(AF_UNIX,SOCK_SEQPACKET|SOCK_CLOEXEC,0);if(fd<0)return 75;struct identity before,after;
    if(connect(fd,(void*)&addr,sizeof addr)||service_peer(fd,argv,&before)){close(fd);return 75;}
    struct timeval bound={30,0};if(setsockopt(fd,SOL_SOCKET,SO_RCVTIMEO,&bound,sizeof bound)){close(fd);return 75;}
    char body[512],reply[512],sha[65],expected[256];int n=snprintf(body,sizeof body,"%s %s %s %s %s",argv[9],argv[10],argv[11],argv[12],argv[13]);digest_bytes(body,(size_t)n,sha);
    if(send(fd,body,(size_t)n,MSG_NOSIGNAL)!=n){close(fd);return 75;}ssize_t got=recv(fd,reply,sizeof reply-1,MSG_TRUNC);
    bad=service_peer(fd,argv,&after)||!identity_equal(&before,&after);close(fd);if(bad||got<=0||(size_t)got>=sizeof reply-1)return 75;reply[got]=0;
    for(int rc=0;rc<=255;rc++){int count=snprintf(expected,sizeof expected,"{\"ok\":%s,\"requestId\":\"%s\",\"requestSha256\":\"%s\",\"exitCode\":%d}\n",rc==0?"true":"false",argv[11],sha,rc);if(got==count&&!memcmp(reply,expected,(size_t)got)){if(fwrite(reply,1,(size_t)got,stdout)!=(size_t)got)return 74;return rc;}}
    return 75;
}

/* Exact read-only socket/record proof. A pathname, bare PID or init status is
 * never sufficient. Scope is identical to the existing service-host argv. */
static int service_host_probe(char **argv,struct identity *owner){
    if(!service_scope_valid(9,argv,0)||migration_boot(boot))return -1;
    int base=checked_directory(argv[2]),fd=-1,result=-1;char *expected=NULL;size_t size=0;struct stat st;
    if(base<0)return -1;
    if(fstatat(base,"control",&st,AT_SYMLINK_NOFOLLOW)||!S_ISSOCK(st.st_mode)||st.st_uid!=geteuid()||st.st_nlink!=1||(st.st_mode&07777)!=0700)goto done;
    struct sockaddr_un address;memset(&address,0,sizeof address);address.sun_family=AF_UNIX;
    if(snprintf(address.sun_path,sizeof address.sun_path,"%s/control",argv[2])>=(int)sizeof address.sun_path)goto done;
    fd=socket(AF_UNIX,SOCK_SEQPACKET|SOCK_CLOEXEC,0);if(fd<0)goto done;
    struct identity before,after;
    if(connect(fd,(void*)&address,sizeof address)||service_peer(fd,argv,&before))goto done;
    struct timeval bound={2,0};if(setsockopt(fd,SOL_SOCKET,SO_RCVTIMEO,&bound,sizeof bound)||send(fd,"STATUS",6,MSG_NOSIGNAL)!=6)goto done;
    char reply[65536];ssize_t got=recv(fd,reply,sizeof reply,MSG_TRUNC);
    if(got<=0||(size_t)got>=sizeof reply||service_peer(fd,argv,&after)||!identity_equal(&before,&after))goto done;
    FILE *f=open_memstream(&expected,&size);if(!f)goto done;
    fputs("BROray-independent-app-service/1\n",f);for(int i=2;i<9;i++)fprintf(f,"%s\n",argv[i]);identity_json(f,&before);fputc('\n',f);
    if(fclose(f)||size!=(size_t)got||memcmp(expected,reply,size)||bg_record_exact(base,"host.record",expected,size))goto done;
    *owner=before;result=0;
done:
    free(expected);if(fd>=0)close(fd);close(base);return result;
}
static int service_host_status(int argc,char **argv){
    struct identity owner;if(argc!=9||service_host_probe(argv,&owner))return service_error("LAUNCHER_READINESS_UNCONFIRMED");
    printf("{\"ok\":true,\"generationId\":\"%s\",\"appActionsAuthorized\":false,\"host\":",argv[4]);identity_json(stdout,&owner);puts("}");return 0;
}

/* Read-only terminal proof for a protected service-operation controller.
 * A caller must separately authenticate its generation/operation and retain
 * installation exclusion. This proves only this host's terminal history;
 * it grants no start, signal, readiness, or fence-retirement authority. */
static int service_host_retired_proof(int argc,char **argv,int inherited,int *retained,char receipt_sha[65]){
    if(argc!=10||!service_scope_valid(9,argv,0)||!hex64(argv[9]))return 64;
    int base=-1,named=-1,result=75;struct stat held,again;
    char canonical[PATH_MAX],want[PATH_MAX],actual[65],terminal_text[512];
    char *host=NULL,*scope=NULL;size_t host_size=0,scope_size=0;
    if(migration_boot(boot)||!realpath(argv[6],canonical)||strcmp(canonical,argv[6]))goto done;
    const char *prefix=strcmp(argv[6],"/")?argv[6]:"";
    int n=snprintf(want,sizeof want,"%s/opt/var/lib/broray-updater/hosts/%s",prefix,argv[4]);
    if(n<0||n>=(int)sizeof want||strcmp(want,argv[2]))goto done;
    n=snprintf(want,sizeof want,"%s/opt/var/lib/broray-updater/generations/%s",prefix,argv[4]);
    if(n<0||n>=(int)sizeof want||strcmp(want,argv[3]))goto done;
    base=inherited<0?checked_directory(argv[2]):dup(inherited);
    if(base<0||fstat(base,&held)||(held.st_mode&07777)!=0700||
       flock(base,LOCK_EX|LOCK_NB)||safe_bytes_at(base,"host.record",&host,&host_size)||memchr(host,0,host_size))goto done;
    digest_bytes(host,host_size,actual);if(strcmp(actual,argv[9]))goto done;
    FILE *f=open_memstream(&scope,&scope_size);if(!f)goto done;
    fputs("BROray-independent-app-service/1\n",f);for(int i=2;i<9;i++)fprintf(f,"%s\n",argv[i]);
    if(fclose(f)||host_size<=scope_size+1||memcmp(host,scope,scope_size)||host[scope_size]!='{'||
       host[host_size-1]!='\n'||memchr(host+scope_size,'\n',host_size-scope_size-1))goto done;
    /* Terminal evidence belongs to the host's pinned birth boot, which can
     * precede a restart of the protected installer. This is read-only history,
     * never live-process or readiness authority. The host hash was checked
     * above; require its one canonical boot field and the same ledger boot. */
    const char *born=strstr(host+scope_size,"\"bootId\":\"");char historical_boot[64];
    if(!born||strstr(born+10,"\"bootId\":\""))goto done;
    born+=10;if(strlen(born)<37||born[36]!='"')goto done;
    memcpy(historical_boot,born,36);historical_boot[36]=0;if(!token(historical_boot,63))goto done;
    /* Existing verifier binds the immutable generation ledger, its RETIRE
     * receipt, the exact host hash and every completed host request. A free
     * directory lock or a receipt pathname alone is never sufficient. */
    if(service_retirement_text_at(base,argv,host,host_size,terminal_text,historical_boot)||
       bg_record_exact(base,"retirement.receipt",terminal_text,strlen(terminal_text))||
       bg_record_exact(base,"host.record",host,host_size))goto done;
    named=checked_directory(argv[2]);
    if(named<0||fstat(named,&again)||again.st_dev!=held.st_dev||again.st_ino!=held.st_ino||
       again.st_mode!=held.st_mode||again.st_uid!=held.st_uid)goto done;
    digest_bytes(terminal_text,strlen(terminal_text),receipt_sha);
    if(retained){*retained=base;base=-1;}
    result=0;
done:
    free(host);free(scope);if(named>=0)close(named);if(base>=0)close(base);
    return result?service_error("LAUNCHER_TERMINAL_UNCONFIRMED"):0;
}
static int service_host_retired(int argc,char **argv){
    char sha[65];int rc=service_host_retired_proof(argc,argv,-1,NULL,sha);if(rc)return rc;
    printf("{\"ok\":true,\"generationId\":\"%s\",\"hostRetired\":true,\"hostRecordSha256\":\"%s\",\"retirementReceiptSha256\":\"%s\",\"signalsAuthorized\":false,\"appActionsAuthorized\":false}\n",argv[4],argv[9],sha);return 0;
}

/* Normal service-stop settlement keeps BOTH existing lifetime exclusions
 * through the canonical state/fence write. No mutex or evidence is created.
 * All sibling generations must have full immutable retirement proof. */
static int service_stop_inherited(const char *key){
    const char *value=getenv(key);char *end=NULL;long fd;
    if(!value||!*value)return -1;errno=0;fd=strtol(value,&end,10);
    return errno||!end||*end||fd<3||fd>INT_MAX?-1:dup((int)fd);
}
static int service_stop_generations(int parent,const char *path,int lock,const char *expected_manifest){
    struct stat held,named;
    if(fstat(lock,&held)||!S_ISREG(held.st_mode)||held.st_uid!=geteuid()||held.st_nlink!=1||
       (held.st_mode&07777)!=0600||held.st_size||
       fstatat(parent,".generation-lifetime.lock",&named,AT_SYMLINK_NOFOLLOW)||
       held.st_dev!=named.st_dev||held.st_ino!=named.st_ino||held.st_mode!=named.st_mode||
       held.st_uid!=named.st_uid||named.st_nlink!=1||named.st_size||flock(lock,LOCK_EX|LOCK_NB))return -1;
    DIR *d=directory_stream(parent);if(!d)return -1;struct dirent *e;int bad=0;unsigned count=0;errno=0;
    while((e=readdir(d))){
        if(!strcmp(e->d_name,".")||!strcmp(e->d_name,"..")||!strcmp(e->d_name,".generation-lifetime.lock"))continue;
        char domain[PATH_MAX];struct stat st;
        if(!token(e->d_name,64)||fstatat(parent,e->d_name,&st,AT_SYMLINK_NOFOLLOW)||!S_ISDIR(st.st_mode)||
           st.st_uid!=geteuid()||(st.st_mode&07777)!=0700||snprintf(domain,sizeof domain,"%s/%s",path,e->d_name)>=(int)sizeof domain){bad=1;break;}
        int fd=checked_directory(domain);struct retired_record r;
        if(fd<0){bad=1;break;}
        bad=retirement_valid(fd,domain,&r)?generation_boot_retirement_valid(fd,domain,expected_manifest,NULL):strcmp(r.gen,e->d_name);close(fd);if(bad)break;count++;errno=0;
    }
    if(!e&&errno)bad=1;closedir(d);return bad||!count?-1:0;
}
static int replacement_service_code_verify(const char *root,const char *origin,const char *start);
static int service_stop_guard(int argc,char **argv){
    /* HOST DOMAIN GEN MANIFEST LIVE ASH ASH_SHA HOST_SHA ORIGIN MIGRATION STOP_OP NONCE */
    int check=argc>1&&!strcmp(argv[1],"service-stop-check");
    if(argc!=14||!service_scope_valid(9,argv,0)||!hex64(argv[9])||!token(argv[10],96)||
       !hex64(argv[11])||!token(argv[12],96)||!hex64(argv[5])||!token(argv[13],32)||strlen(argv[13])!=32)return 64;
    int state=-1,guard=-1,parent=-1,lock=-1,host=-1,ash=-1,fs=-1,inherited_host=-1,result=75;
    char statepath[PATH_MAX],generations[PATH_MAX],origin[PATH_MAX],migration[PATH_MAX],code[PATH_MAX],controller[PATH_MAX],nativepath[PATH_MAX];
    char path[PATH_MAX+80],app[PATH_MAX],global[PATH_MAX],updater[PATH_MAX],legacy[PATH_MAX],ram[PATH_MAX],guardpath[PATH_MAX];
    char native[65],host_receipt[65],field[160],fdtext[32];const char *prefix=strcmp(argv[6],"/")?argv[6]:"";
#define SS_PATH(dest,fmt,...) do{int n=snprintf(dest,sizeof dest,fmt,__VA_ARGS__);if(n<0||n>=(int)sizeof dest)goto done;}while(0)
    SS_PATH(statepath,"%s/opt/var/lib/broray",prefix);SS_PATH(generations,"%s/opt/var/lib/broray-updater/generations",prefix);
    SS_PATH(origin,"%s/operations/%s",statepath,argv[10]);SS_PATH(migration,"%s/platform-migration",origin);
    SS_PATH(code,"%s/platform-recovery-code/code",origin);SS_PATH(controller,"%s/lib/operation-coordinator.sh",code);
    SS_PATH(nativepath,"%s/platform-bootguard/runtime",origin);SS_PATH(guardpath,"%s/bin/broray-ops-guard",code);
    SS_PATH(app,"%s/opt/broray",prefix);SS_PATH(global,"%s/opt/var/lock/broray/global-operation.lock",prefix);
    SS_PATH(updater,"%s/opt/var/lib/broray-updater",prefix);SS_PATH(legacy,"%s/tmp/broray-global-operation.lock",prefix);
    SS_PATH(ram,"%s/tmp/broray-operations",prefix);SS_PATH(path,"%s/opt/bin:%s/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin",prefix,prefix);
#undef SS_PATH
    state=checked_directory(statepath);if(state<0||(guard=recovery_inherited_guard(state))<0)goto done;
    parent=checked_directory(generations);if(parent<0)goto done;
    lock=check?service_stop_inherited("BRORAY_SERVICE_STOP_GENERATIONS_FD"):openat(parent,".generation-lifetime.lock",O_RDWR|O_NOFOLLOW|O_CLOEXEC);
    if(lock<0||service_stop_generations(parent,generations,lock,argv[5]))goto done;
    inherited_host=check?service_stop_inherited("BRORAY_SERVICE_STOP_HOST_FD"):checked_directory(argv[2]);
    if(inherited_host<0)goto done;
    if(!check){
        /* RETIRE precedes the independent host's durable terminal receipt.
         * Wait only for its lifetime exclusion on this same descriptor; the
         * complete immutable proof below still decides whether it stopped. */
        uint64_t until=millis()+2000;
        while(flock(inherited_host,LOCK_EX|LOCK_NB)){
            if(errno!=EWOULDBLOCK||millis()>=until)goto done;
            struct timespec pause={0,10000000L};nanosleep(&pause,NULL);
        }
    }
    if(service_host_retired_proof(10,argv,inherited_host,&host,host_receipt))goto done;
    /* Historical RETIRE is read-only; it authenticates the entire ledger,
     * exact stop operation/nonce and original platform launch before any exec. */
    char *retire[]={argv[0],"control",argv[3],"RETIRE",argv[4],argv[5],argv[12],argv[13],NULL};
    if(retired_reply(retire,0)||peer_executable_hash(getpid(),native))goto done;
    char *launch=strstr(snapshot,"\"platformLaunch\":{");if(!launch)goto done;
    snprintf(field,sizeof field,"\"nativeSha256\":\"%s\"",native);if(!strstr(launch,field))goto done;
    snprintf(field,sizeof field,"\"operationId\":\"%s\"",argv[10]);if(!strstr(launch,field))goto done;
    int origin_fd=checked_directory(origin);if(origin_fd<0)goto done;
    int replacement=bg_exists(origin_fd,"platform-replacement-service.json"),replacement_anchor=bg_exists(origin_fd,"platform-replacement-service.anchor");close(origin_fd);
    if(replacement<0||replacement_anchor<0||replacement!=replacement_anchor)goto done;
    if(replacement){
        if(replacement_service_code_verify(argv[6],argv[10],argv[11])||
           snprintf(code,sizeof code,"%s/platform-replacement-code/code",origin)>=(int)sizeof code||
           snprintf(controller,sizeof controller,"%s/lib/operation-coordinator.sh",code)>=(int)sizeof controller||
           snprintf(guardpath,sizeof guardpath,"%s/bin/broray-ops-guard",code)>=(int)sizeof guardpath||
           snprintf(nativepath,sizeof nativepath,"%s/runtimes/%s/runtime",updater,native)>=(int)sizeof nativepath)goto done;
    }else{
        char *verify[]={argv[0],"recovery-code-verify",origin,argv[6],migration,argv[11],NULL};
        if(recovery_code_impl(6,verify,0))goto done;
    }
    fs=migration_directory("/");if(fs<0)goto done;ash=migration_relative(fs,argv[7]+1);
    if(ash<0||service_interpreter_valid(ash,argv[7],argv[8])||service_stop_generations(parent,generations,lock,argv[5]))goto done;
    if(check){
        printf("{\"ok\":true,\"phase\":\"SERVICE_STOP_EXCLUSION_VERIFIED\",\"generationId\":\"%s\",\"hostReceiptSha256\":\"%s\",\"serviceStopped\":true,\"platformReady\":false}\n",argv[4],host_receipt);
        result=0;goto done;
    }
    /* Only this already-authenticated retained coordinator is executed. It
     * rechecks all proofs using inherited descriptors before/after mutation.
     * Descendant state publishers retain the flocks if this process dies. */
    if(clearenv())goto done;
    const char *keys[]={"PATH","LC_ALL","BRORAY_ROOT","BRORAY_STATE_ROOT","BRORAY_OPS_CODE_ROOT","BRORAY_OPS_GUARD","BRORAY_OPS_ASH","BRORAY_ROUTES_API_LOCK","BRORAY_OPS_UPDATER_ROOT","BRORAY_LEGACY_GLOBAL_LOCK","BRORAY_OPS_RAM_ROOT","BRORAY_OPS_GUARD_HELD","BRORAY_OPS_GENERATION"};
    const char *values[]={path,"C",app,statepath,code,guardpath,argv[7],global,updater,legacy,ram,"1",nativepath};
    for(unsigned i=0;i<sizeof keys/sizeof keys[0];i++)if(setenv(keys[i],values[i],1))goto done;
    snprintf(fdtext,sizeof fdtext,"%d",lock);if(setenv("BRORAY_SERVICE_STOP_GENERATIONS_FD",fdtext,1))goto done;
    snprintf(fdtext,sizeof fdtext,"%d",host);if(setenv("BRORAY_SERVICE_STOP_HOST_FD",fdtext,1))goto done;
    int retained[]={guard,lock,host,ash};
    for(unsigned i=0;i<sizeof retained/sizeof retained[0];i++){int flags=fcntl(retained[i],F_GETFD);if(flags<0||fcntl(retained[i],F_SETFD,flags&~FD_CLOEXEC))goto done;}
    char executable[64];snprintf(executable,sizeof executable,"/proc/self/fd/%d",ash);
    char *command[]={"ash",controller,"platform-preflight-stop-settle",argv[12],argv[13],NULL};
    execv(executable,command);
done:
    if(state>=0)close(state);if(guard>=0)close(guard);if(parent>=0)close(parent);if(lock>=0)close(lock);
    if(host>=0)close(host);if(ash>=0)close(ash);if(fs>=0)close(fs);if(inherited_host>=0)close(inherited_host);
    return result?service_error("STOP_SETTLEMENT_EXCLUSION_UNCONFIRMED"):0;
}

/* The enclosing replacement transaction holds all three exclusions. Restore
 * only the exact inodes retained by this install, never a guessed file copy. */
static int service_replacement_rollback_files(int op,int live,const char *op_path,int install,
        int installed,const char *intent_sha,const char *backup_sha,const char *target_sha,
        const struct migration_file before[MIGRATION_FILES],const struct migration_file after[MIGRATION_FILES],
        int parents[MIGRATION_FILES],char entries[MIGRATION_FILES][NAME_MAX+1],int *replayed){
    int base=-1,result=-1;char path[PATH_MAX],record[384],sha[65],binding[128],receipt[320],anchor[128],receipt_sha[65];
    int n=snprintf(record,sizeof record,"BROray-platform-replacement-rollback/1\nROLLING_BACK\n%s\n%s\n%s\n",intent_sha,backup_sha,target_sha);
    if(n<0||n>=(int)sizeof record)return -1;digest_bytes(record,(size_t)n,sha);
    int bn=snprintf(binding,sizeof binding,"BROray-platform-replacement-rollback-binding/1\n%s\n",sha);
    if(bn<0||bn>=(int)sizeof binding)return -1;
    int exists=bg_exists(op,"platform-replacement-rollback"),bound=bg_exists(op,"platform-replacement-rollback.record");
    if(exists<0||bound<0||exists!=bound||snprintf(path,sizeof path,"%s/platform-replacement-rollback",op_path)>=(int)sizeof path)return -1;
    if(!exists){
        /* Validate every current/previous inode before making any rollback
         * intent durable. Unknown changes leave the entire installation alone. */
        for(int i=0;i<MIGRATION_FILES;i++){
            char rejected[128];snprintf(rejected,sizeof rejected,".broray-pt-%s-%d.rejected",intent_sha,i);
            if(bg_exists(parents[i],rejected)!=0||pt_install_entry(install,parents[i],entries[i],i,intent_sha,&before[i],&after[i],installed,0))return -1;
        }
        if(migration_record(op,"platform-replacement-rollback.record",binding,(size_t)bn,1)||mkdirat(op,"platform-replacement-rollback",0700)||fsync(op))return -1;
    }
    base=checked_directory(path);
    if(base<0||(!exists&&bg_empty(base))||pt_rollback_names(base)||
       migration_record(op,"platform-replacement-rollback.record",binding,(size_t)bn,0)||migration_record(base,"intent.record",record,(size_t)n,!exists))goto done;
    int complete=bg_exists(base,"restored.receipt"),anchored=bg_exists(base,"restored.anchor");
    if(complete<0||anchored<0||complete!=anchored)goto done;
    for(int i=0;i<MIGRATION_FILES;i++)if(pt_rollback_entry(base,install,parents[i],entries[i],i,intent_sha,sha,&before[i],&after[i],installed,complete,0))goto done;
    for(int i=0;i<MIGRATION_FILES;i++){
        if(bg_record_exact(op,"platform-replacement-rollback.record",binding,(size_t)bn)||
           bg_record_exact(base,"intent.record",record,(size_t)n)||pt_rollback_names(base)||pt_install_names(install)||
           pt_rollback_entry(base,install,parents[i],entries[i],i,intent_sha,sha,&before[i],&after[i],installed,complete,1))goto done;
    }
    int rn=snprintf(receipt,sizeof receipt,"BROray-platform-replacement-restored/1\n%s\n%s\nservice-state-not-restored\n",sha,backup_sha);
    if(rn<0||rn>=(int)sizeof receipt)goto done;digest_bytes(receipt,(size_t)rn,receipt_sha);
    int an=snprintf(anchor,sizeof anchor,"BROray-platform-replacement-restored-anchor/1\n%s\n",receipt_sha);
    if(an<0||an>=(int)sizeof anchor||pt_inventory(live,before)||
       migration_record(base,"restored.anchor",anchor,(size_t)an,!complete)||migration_record(base,"restored.receipt",receipt,(size_t)rn,!complete)||
       migration_sync_directory(path,base)||pt_rollback_names(base)||pt_inventory(live,before)||
       bg_record_exact(op,"platform-replacement-rollback.record",binding,(size_t)bn)||bg_record_exact(base,"intent.record",record,(size_t)n))goto done;
    for(int i=0;i<MIGRATION_FILES;i++)if(pt_rollback_entry(base,install,parents[i],entries[i],i,intent_sha,sha,&before[i],&after[i],installed,1,0))goto done;
    *replayed=complete;result=0;
done:if(base>=0)close(base);return result;
}

/* The caller retains operation, generation and host exclusion, and has
 * verified the complete immutable replacement backup. Seven-file writes use
 * the existing inode-bound, no-replace transaction primitive. */
static int service_replacement_install(char **argv,int op,int live,const char *op_path,
                                      const struct migration_file before[MIGRATION_FILES],const char *backup_sha,int *installed_replay){
    int rollback=!strcmp(argv[1],"replacement-rollback");
    int source=-1,stage=-1,base=-1,result=75,replay=0,parents[MIGRATION_FILES];
    for(int i=0;i<MIGRATION_FILES;i++)parents[i]=-1;
    struct migration_file after[MIGRATION_FILES],manifest_file;memset(after,0,sizeof after);memset(&manifest_file,0,sizeof manifest_file);
    char path[PATH_MAX],entries[MIGRATION_FILES][NAME_MAX+1],intent[768],intent_sha[65],binding[128],ready[192],anchor[128],ready_sha[65];
    char target_intent[768],target_sha[65],target_binding[128];
    if(rollback){
        if(snprintf(path,sizeof path,"%s/platform-replacement-target",op_path)>=(int)sizeof path)goto done;
        source=checked_directory(path);
    }else source=migration_directory(argv[15]);
    if(source<0||migration_read(source,rollback?"manifest.record":"SHA256SUMS",&manifest_file,0)||strcmp(manifest_file.sha,argv[12])||(rollback&&manifest_file.mode!=0600))goto done;
    for(int i=0;i<MIGRATION_FILES;i++){
        char name[32];snprintf(name,sizeof name,"file-%d",i);
        if(migration_read(source,rollback?name:migration_paths[i],&after[i],0)||after[i].mode!=(rollback?0600:0755))goto done;
        after[i].mode=0755;
    }
    unsigned seen=0;size_t offset=0;
    while(offset<manifest_file.size){
        char *line=manifest_file.bytes+offset,*nl=memchr(line,'\n',manifest_file.size-offset);if(!nl)goto done;
        int matched=0;for(int i=0;i<MIGRATION_FILES;i++){
            char row[256];int n=snprintf(row,sizeof row,"%s  %s",after[i].sha,migration_paths[i]);
            if(n==(int)(nl-line)&&!memcmp(line,row,(size_t)n)&&!(seen&(1U<<i))){seen|=1U<<i;matched=1;break;}
        }
        if(!matched)goto done;offset=(size_t)(nl-manifest_file.bytes)+1;
    }
    if(seen!=((1U<<MIGRATION_FILES)-1))goto done;
    int tn=snprintf(target_intent,sizeof target_intent,"BROray-platform-replacement-target/1\n%s\n%s\n%s\n%s\n",argv[10],argv[11],argv[12],backup_sha);
    if(tn<0||tn>=(int)sizeof target_intent)goto done;digest_bytes(target_intent,(size_t)tn,target_sha);
    int tbn=snprintf(target_binding,sizeof target_binding,"BROray-platform-replacement-target-binding/1\n%s\n",target_sha);
    if(tbn<0||tbn>=(int)sizeof target_binding)goto done;
    int exists=bg_exists(op,"platform-replacement-target"),bound=bg_exists(op,"platform-replacement-target.record");
    if(exists<0||bound<0||exists!=bound||(rollback&&!exists))goto done;
    if(snprintf(path,sizeof path,"%s/platform-replacement-target",op_path)>=(int)sizeof path)goto done;
    if(!exists){
        if(pt_inventory(live,before)||migration_record(op,"platform-replacement-target.record",target_binding,(size_t)tbn,1)||
           mkdirat(op,"platform-replacement-target",0700)||fsync(op))goto done;
    }
    stage=checked_directory(path);if(stage<0||(!exists&&bg_empty(stage)))goto done;
    if(migration_record(op,"platform-replacement-target.record",target_binding,(size_t)tbn,0)||
       migration_record(stage,"intent.record",target_intent,(size_t)tn,!exists)||
       migration_record(stage,"manifest.record",manifest_file.bytes,manifest_file.size,!exists))goto done;
    const char *names[MIGRATION_FILES+3]={"intent.record","manifest.record","ready.receipt"};char files[MIGRATION_FILES][32];
    for(int i=0;i<MIGRATION_FILES;i++){
        snprintf(files[i],sizeof files[i],"file-%d",i);names[3+i]=files[i];
        if(migration_record(stage,files[i],after[i].bytes,after[i].size,!exists))goto done;
    }
    if(migration_record(stage,"ready.receipt",target_binding,(size_t)tbn,!exists)||rc_names(stage,names,MIGRATION_FILES+3)||migration_sync_directory(path,stage))goto done;
    int in=snprintf(intent,sizeof intent,"BROray-platform-replacement-install/1\nINSTALLING\n%s\n%s\n%s\n%s\n%s\n",argv[10],argv[11],argv[12],backup_sha,target_sha);
    if(in<0||in>=(int)sizeof intent)goto done;digest_bytes(intent,(size_t)in,intent_sha);
    int bn=snprintf(binding,sizeof binding,"BROray-platform-replacement-install-binding/1\n%s\n",intent_sha);
    if(bn<0||bn>=(int)sizeof binding)goto done;
    exists=bg_exists(op,"platform-replacement-install");bound=bg_exists(op,"platform-replacement-install.record");
    if(exists<0||bound<0||exists!=bound||(rollback&&!exists))goto done;
    if(!rollback&&(bg_exists(op,"platform-replacement-rollback")!=0||bg_exists(op,"platform-replacement-rollback.record")!=0))goto done;
    if(snprintf(path,sizeof path,"%s/platform-replacement-install",op_path)>=(int)sizeof path)goto done;
    if(!exists){
        if(pt_inventory(live,before)||migration_record(op,"platform-replacement-install.record",binding,(size_t)bn,1)||
           mkdirat(op,"platform-replacement-install",0700)||fsync(op))goto done;
    }
    base=checked_directory(path);if(base<0||(!exists&&bg_empty(base))||pt_install_names(base)||
       migration_record(op,"platform-replacement-install.record",binding,(size_t)bn,0)||migration_record(base,"intent.record",intent,(size_t)in,!exists))goto done;
    int completed=bg_exists(base,"installed.receipt"),anchored=bg_exists(base,"installed.anchor");
    if(completed<0||anchored<0||completed!=anchored)goto done;replay=completed;
    for(int i=0;i<MIGRATION_FILES;i++){
        parents[i]=bg_parent(live,migration_paths[i],entries[i]);
        if(parents[i]<0)goto done;
    }
    if(rollback){
        int rn=snprintf(ready,sizeof ready,"BROray-platform-replacement-installed/1\n%s\n%s\n",intent_sha,argv[12]);
        if(rn<0||rn>=(int)sizeof ready)goto done;digest_bytes(ready,(size_t)rn,ready_sha);
        int an=snprintf(anchor,sizeof anchor,"BROray-platform-replacement-installed-anchor/1\n%s\n",ready_sha);
        if(an<0||an>=(int)sizeof anchor||(completed&&(bg_record_exact(base,"installed.receipt",ready,(size_t)rn)||bg_record_exact(base,"installed.anchor",anchor,(size_t)an)))||
           service_replacement_rollback_files(op,live,op_path,base,completed,intent_sha,backup_sha,target_sha,before,after,parents,entries,installed_replay))goto done;
        result=0;goto done;
    }
    for(int i=0;i<MIGRATION_FILES;i++)if(pt_install_entry(base,parents[i],entries[i],i,intent_sha,&before[i],&after[i],completed,0))goto done;
    /* All before/staged/current inodes are validated before the first write.
     * Every mutation has its own durable intent and exact completion record. */
    for(int i=0;i<MIGRATION_FILES;i++){
        if(bg_record_exact(op,"platform-replacement-install.record",binding,(size_t)bn)||
           bg_record_exact(base,"intent.record",intent,(size_t)in)||
           bg_record_exact(stage,files[i],after[i].bytes,after[i].size)||
           pt_install_entry(base,parents[i],entries[i],i,intent_sha,&before[i],&after[i],completed,1))goto done;
    }
    int rn=snprintf(ready,sizeof ready,"BROray-platform-replacement-installed/1\n%s\n%s\n",intent_sha,argv[12]);
    if(rn<0||rn>=(int)sizeof ready)goto done;digest_bytes(ready,(size_t)rn,ready_sha);
    int an=snprintf(anchor,sizeof anchor,"BROray-platform-replacement-installed-anchor/1\n%s\n",ready_sha);
    if(an<0||an>=(int)sizeof anchor||pt_inventory(live,after)||
       migration_record(base,"installed.anchor",anchor,(size_t)an,!completed)||migration_record(base,"installed.receipt",ready,(size_t)rn,!completed)||
       pt_install_names(base)||migration_sync_directory(path,base)||pt_inventory(live,after))goto done;
    for(int i=0;i<MIGRATION_FILES;i++)if(pt_install_entry(base,parents[i],entries[i],i,intent_sha,&before[i],&after[i],1,0))goto done;
    *installed_replay=replay;result=0;
done:
    for(int i=0;i<MIGRATION_FILES;i++){free(after[i].bytes);if(parents[i]>=0)close(parents[i]);}free(manifest_file.bytes);
    if(base>=0)close(base);if(stage>=0)close(stage);if(source>=0)close(source);
    return result?service_error(rollback?"PLATFORM_REPLACEMENT_ROLLBACK_UNCONFIRMED":"PLATFORM_REPLACEMENT_INSTALL_UNCONFIRMED"):0;
}

/* Same-boot replacement backup. This bounded writer changes only private
 * operation evidence. It does not authorize installation, release the fence,
 * execute a helper, or invent a legacy boot boundary. */
static int service_replacement_backup(int argc,char **argv){
    /* HOST DOMAIN GEN OLD_MANIFEST LIVE ASH ASH_SHA HOST_SHA OP NONCE
     * NEW_MANIFEST OLD_NATIVE STATE_SHA */
    int rollback=argc>1&&!strcmp(argv[1],"replacement-rollback");
    int start=argc>1&&!strcmp(argv[1],"replacement-start-intent");
    int install=rollback||start||(argc>1&&!strcmp(argv[1],"replacement-install"));
    if(argc!=((install&&!rollback)?16:15)||!service_scope_valid(9,argv,0)||!hex64(argv[9])||!token(argv[10],96)||
       !token(argv[11],32)||strlen(argv[11])!=32||!hex64(argv[12])||!hex64(argv[13])||!hex64(argv[14])||
       ((install&&!rollback)&&!migration_path(argv[15])))return 64;
    int state=-1,guard=-1,parent=-1,lock=-1,host=-1,op=-1,live=-1,base=-1,result=75,replay=0;
    char statepath[PATH_MAX],generations[PATH_MAX],oppath[PATH_MAX],fence[PATH_MAX],want[PATH_MAX],backup[PATH_MAX];
    char host_receipt[65],native[65],inventory_sha[65],intent_sha[65],receipt[192],anchor[128],anchor_sha[65],field[256];
    char *inventory=NULL,*target=NULL,*intent=NULL,*execution=NULL;size_t inventory_size=0,target_size=0,intent_size=0,execution_size=0;
    struct migration_file before[MIGRATION_FILES],operation_state,prior_executor;
    memset(before,0,sizeof before);memset(&operation_state,0,sizeof operation_state);memset(&prior_executor,0,sizeof prior_executor);
    struct identity executor;const char *prefix=strcmp(argv[6],"/")?argv[6]:"";umask(077);
#define RB_PATH(dest,fmt,...) do{int n=snprintf(dest,sizeof dest,fmt,__VA_ARGS__);if(n<0||n>=(int)sizeof dest)goto done;}while(0)
    RB_PATH(statepath,"%s/opt/var/lib/broray",prefix);RB_PATH(oppath,"%s/operations/%s",statepath,argv[10]);
    RB_PATH(generations,"%s/opt/var/lib/broray-updater/generations",prefix);
    RB_PATH(fence,"%s/opt/var/lock/broray/global-operation.lock",prefix);RB_PATH(want,"%s/fence",oppath);
    RB_PATH(backup,"%s/platform-replacement-backup",oppath);
#undef RB_PATH
    state=checked_directory(statepath);op=checked_directory(oppath);live=migration_directory(argv[6]);
    if(state<0||op<0||live<0||(guard=recovery_inherited_guard(state))<0||migration_boot(boot)||
       peer_executable_hash(getpid(),native)||capture(getpid(),&executor))goto done;
    char link[PATH_MAX];ssize_t linked=readlink(fence,link,sizeof link);
    if(linked!=(ssize_t)strlen(want)||memcmp(link,want,(size_t)linked)||
       migration_read(op,"state.json",&operation_state,0)||operation_state.mode!=0600||
       strcmp(operation_state.sha,argv[14])||memchr(operation_state.bytes,0,operation_state.size))goto done;
    /* Canonical coordinator state is pinned in full, not a caller's health
     * boolean. The exact generationStop object is reconstructed below from
     * independently verified terminal lineage and the seven live files. */
    if(!strstr(operation_state.bytes,"\"operation\":\"system:platform-preflight\"")||
       !strstr(operation_state.bytes,"\"running\":true")||!strstr(operation_state.bytes,"\"phase\":\"STOPPED\""))goto done;
    snprintf(field,sizeof field,"\"expectedPlatformManifestSha256\":\"%s\"",argv[12]);if(!strstr(operation_state.bytes,field))goto done;
    snprintf(field,sizeof field,"\"stopNonce\":\"%s\"",argv[11]);if(!strstr(operation_state.bytes,field))goto done;
    parent=checked_directory(generations);if(parent<0)goto done;
    lock=openat(parent,".generation-lifetime.lock",O_RDWR|O_NOFOLLOW|O_CLOEXEC);
    if(lock<0||service_stop_generations(parent,generations,lock,argv[5]))goto done;
    host=checked_directory(argv[2]);if(host<0)goto done;
    uint64_t until=millis()+2000;
    while(flock(host,LOCK_EX|LOCK_NB)){
        if(errno!=EWOULDBLOCK||millis()>=until)goto done;
        struct timespec pause={0,10000000L};nanosleep(&pause,NULL);
    }
    if(service_host_retired_proof(10,argv,host,NULL,host_receipt))goto done;
    char *retire[]={argv[0],"control",argv[3],"RETIRE",argv[4],argv[5],argv[10],argv[11],NULL};
    if(retired_reply(retire,0))goto done;
    char *launch=strstr(snapshot,"\"platformLaunch\":{");if(!launch)goto done;
    snprintf(field,sizeof field,"\"nativeSha256\":\"%s\"",argv[13]);if(!strstr(launch,field))goto done;
    char *super=strstr(snapshot,"\"supervisor\":"),*up=strstr(snapshot,",\"updater\":"),*end=strstr(snapshot,",\"stopOperationId\":");
    if(!super||!up||!end||up<=super||end<=up)goto done;super+=strlen("\"supervisor\":");
    if(install){base=checked_directory(backup);if(base<0)goto done;}
    FILE *mf=open_memstream(&inventory,&inventory_size);if(!mf)goto done;
    int bad=0;static const int sorted[MIGRATION_FILES]={0,1,2,3,5,4,6};
    for(int j=0;j<MIGRATION_FILES;j++){
        int i=sorted[j];char name[32];snprintf(name,sizeof name,"before-%d",i);
        if(migration_read(install?base:live,install?name:migration_paths[i],&before[i],0)||
           before[i].mode!=(install?0600:0755)){bad=1;break;}before[i].mode=0755;
        fprintf(mf,"%s  %s\n",before[i].sha,migration_paths[i]);
    }
    if(fclose(mf))bad=1;if(bad)goto done;digest_bytes(inventory,inventory_size,inventory_sha);if(strcmp(inventory_sha,argv[5]))goto done;
    FILE *tf=open_memstream(&target,&target_size);if(!tf)goto done;
    fprintf(tf,"\"generationStop\":{\"contract\":\"broray-platform-generation-stop/1\",\"generationId\":\"%s\",\"platformManifestSha256\":\"%s\",\"nativeSha256\":\"%s\",\"supervisor\":",argv[4],argv[5],argv[13]);
    fwrite(super,1,(size_t)(up-super),tf);fwrite(up,1,(size_t)(end-up),tf);fputs(",\"platformFiles\":[",tf);
    for(int j=0;j<MIGRATION_FILES;j++){int i=sorted[j];fprintf(tf,"%s{\"path\":\"%s\",\"value\":{\"sha256\":\"%s\",\"executable\":true}}",j?",":"",migration_paths[i]+4,before[i].sha);}
    fputs("]}",tf);if(fclose(tf)||!strstr(operation_state.bytes,target))goto done;
    int directory=bg_exists(op,"platform-replacement-backup"),bound=bg_exists(op,"platform-replacement-backup.record");
    if(directory<0||bound<0||directory!=bound||(install&&!directory))goto done;replay=directory;
    if(replay){
        if(base<0)base=checked_directory(backup);if(base<0||pt_backup_names(base,before)||migration_read(base,"executor.record",&prior_executor,0)||prior_executor.mode!=0600)goto done;
        execution=prior_executor.bytes;execution_size=prior_executor.size;prior_executor.bytes=NULL;
    }else{
        FILE *ef=open_memstream(&execution,&execution_size);if(!ef)goto done;
        identity_json(ef,&executor);fputc('\n',ef);if(fclose(ef))goto done;
    }
    char execution_sha[65];digest_bytes(execution,execution_size,execution_sha);
    FILE *f=open_memstream(&intent,&intent_size);if(!f)goto done;
    fprintf(f,"{\"schemaVersion\":1,\"contract\":\"broray-platform-replacement-backup/1\",\"operationId\":\"%s\",\"stopNonce\":\"%s\",\"oldGenerationId\":\"%s\",\"oldPlatformManifestSha256\":\"%s\",\"oldNativeSha256\":\"%s\",\"oldServiceWasRunning\":true,\"expectedPlatformManifestSha256\":\"%s\",\"writerNativeSha256\":\"%s\",\"operationStateSha256\":\"%s\",\"hostRetirementSha256\":\"%s\",\"executorSha256\":\"%s\",\"before\":[",argv[10],argv[11],argv[4],argv[5],argv[13],argv[12],native,argv[14],host_receipt,execution_sha);
    for(int i=0;i<MIGRATION_FILES;i++)fprintf(f,"%s{\"path\":\"%s\",\"present\":true,\"mode\":%u,\"sha256\":\"%s\"}",i?",":"",migration_paths[i],before[i].mode,before[i].sha);
    fputs("]}\n",f);if(fclose(f))goto done;digest_bytes(intent,intent_size,intent_sha);
    char binding[128];int bn=snprintf(binding,sizeof binding,"BROray-platform-replacement-backup-binding/1\n%s\n",intent_sha);
    if(bn<0||bn>=(int)sizeof binding||(!install&&pt_inventory(live,before)))goto done;
    if(!replay){
        if(migration_record(op,"platform-replacement-backup.record",binding,(size_t)bn,1)||mkdirat(op,"platform-replacement-backup",0700)||fsync(op))goto done;
        base=checked_directory(backup);if(base<0||bg_empty(base))goto done;
    }
    if(migration_record(op,"platform-replacement-backup.record",binding,(size_t)bn,0)||
       migration_record(base,"intent.json",intent,intent_size,!replay)||migration_record(base,"executor.record",execution,execution_size,!replay))goto done;
    for(int i=0;i<MIGRATION_FILES;i++){char name[32];snprintf(name,sizeof name,"before-%d",i);if(migration_record(base,name,before[i].bytes,before[i].size,!replay))goto done;}
    int rn=snprintf(receipt,sizeof receipt,"BROray-platform-replacement-backup-ready/1\n%s\n%s\n",intent_sha,host_receipt);
    if(rn<0||rn>=(int)sizeof receipt)goto done;digest_bytes(receipt,(size_t)rn,anchor_sha);
    int an=snprintf(anchor,sizeof anchor,"BROray-platform-replacement-backup-anchor/1\n%s\n",anchor_sha);
    if(an<0||an>=(int)sizeof anchor||pt_backup_copies(base,before)||(!install&&pt_inventory(live,before))||
       bg_record_exact(op,"state.json",operation_state.bytes,operation_state.size)||
       service_stop_generations(parent,generations,lock,argv[5])||service_host_retired_proof(10,argv,host,NULL,host_receipt)||
       migration_record(base,"ready.anchor",anchor,(size_t)an,!replay)||migration_record(base,"ready.receipt",receipt,(size_t)rn,!replay)||
       pt_backup_names(base,before)||migration_sync_directory(backup,base)||pt_backup_copies(base,before)||(!install&&pt_inventory(live,before))||
       bg_record_exact(op,"state.json",operation_state.bytes,operation_state.size)||bg_record_exact(op,"platform-replacement-backup.record",binding,(size_t)bn)||
       bg_record_exact(base,"intent.json",intent,intent_size)||bg_record_exact(base,"executor.record",execution,execution_size))goto done;
    linked=readlink(fence,link,sizeof link);if(linked!=(ssize_t)strlen(want)||memcmp(link,want,(size_t)linked))goto done;
    if(install){
        if(service_replacement_install(argv,op,live,oppath,before,intent_sha,&replay)||
           bg_record_exact(op,"state.json",operation_state.bytes,operation_state.size)||
           service_stop_generations(parent,generations,lock,argv[5])||service_host_retired_proof(10,argv,host,NULL,host_receipt)||
           pt_backup_copies(base,before)||bg_record_exact(base,"intent.json",intent,intent_size))goto done;
        linked=readlink(fence,link,sizeof link);if(linked!=(ssize_t)strlen(want)||memcmp(link,want,(size_t)linked))goto done;
        if(start){result=replacement_start_prepare(argv,op,oppath,native);goto done;}
        if(rollback)printf("{\"ok\":true,\"phase\":\"NEEDS_RECOVERY\",\"operationId\":\"%s\",\"platformRestored\":true,\"serviceStateRestored\":false,\"replayed\":%s,\"platformReady\":false,\"activationAllowed\":false}\n",argv[10],replay?"true":"false");
        else printf("{\"ok\":true,\"phase\":\"INSTALLED\",\"operationId\":\"%s\",\"manifestSha256\":\"%s\",\"replayed\":%s,\"platformReady\":false,\"activationAllowed\":false}\n",argv[10],argv[12],replay?"true":"false");
        result=0;goto done;
    }
    printf("{\"ok\":true,\"phase\":\"BACKUP_READY\",\"operationId\":\"%s\",\"intentSha256\":\"%s\",\"replayed\":%s,\"platformReady\":false,\"activationAllowed\":false}\n",argv[10],intent_sha,replay?"true":"false");result=0;
done:
    for(int i=0;i<MIGRATION_FILES;i++)free(before[i].bytes);free(operation_state.bytes);free(prior_executor.bytes);
    free(inventory);free(target);free(intent);free(execution);
    if(base>=0)close(base);if(live>=0)close(live);if(op>=0)close(op);if(host>=0)close(host);if(lock>=0)close(lock);if(parent>=0)close(parent);if(guard>=0)close(guard);if(state>=0)close(state);
    return result?service_replacement_error(start?"PLATFORM_REPLACEMENT_START_INTENT_UNCONFIRMED":rollback?"PLATFORM_REPLACEMENT_ROLLBACK_UNCONFIRMED":install?"PLATFORM_REPLACEMENT_INSTALL_UNCONFIRMED":"PLATFORM_REPLACEMENT_BACKUP_UNCONFIRMED"):0;
}
