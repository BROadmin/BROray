/* Included by broray-ops-supervisor.c. Uses its identity/ledger primitives.
 * An existing daemon is NOT a fork-child. Seize without EXITKILL first; only
 * a stopped, revalidated target and its pinned idle child can be authorised.
 * kill(2) is used once, while the exact target remains in ptrace-stop. All
 * later termination is through EXITKILL on the already traced generations.
 */
static int bound_callback(const char *ash,const char *control,pid_t target,pid_t idle,int *armed) {
    int request[2],reply[2];
    if(pipe(request))return -1;
    if(pipe(reply)){close(request[0]);close(request[1]);return -1;}
    pid_t p=fork(); if(p<0){close(request[0]);close(request[1]);close(reply[0]);close(reply[1]);return -1;}
    if(p==0){
        char readfd[24],writefd[24];
        snprintf(readfd,sizeof readfd,"%d",request[0]);snprintf(writefd,sizeof writefd,"%d",reply[1]);
        int sendfd=fcntl(request[1],F_DUPFD,10),receivefd=fcntl(reply[0],F_DUPFD,10);
        if(sendfd<0||receivefd<0)_exit(74);
        close(request[0]);close(request[1]);close(reply[0]);close(reply[1]);
        if(dup2(sendfd,8)<0||dup2(receivefd,9)<0)_exit(74);
        close(sendfd);close(receivefd);
        if(setenv("BRORAY_BOUND_STOP_READ_FD",readfd,1)||setenv("BRORAY_BOUND_STOP_WRITE_FD",writefd,1))_exit(74);
        int null=open("/dev/null",O_WRONLY);if(null<0)_exit(74);
        if(dup2(null,STDOUT_FILENO)<0)_exit(74);close(null);
        char self[24],a[24],b[24];
        snprintf(self,sizeof self,"%d",getppid());
        snprintf(a,sizeof a,"%d",target);snprintf(b,sizeof b,"%d",idle);
        execl(ash,ash,control,"authorize-service-stop",self,nonce,a,b,(char*)0);
        _exit(74);
    }
    close(request[1]);close(reply[0]);
    int flags=fcntl(request[0],F_GETFL),rc=-1,status;
    if(flags<0||fcntl(request[0],F_SETFL,flags|O_NONBLOCK)<0)goto done;
    char expected[128],received[128],ack[64];size_t have=0;
    int length=snprintf(expected,sizeof expected,"ARM %s %d %d\n",nonce,target,idle);
    int acklen=snprintf(ack,sizeof ack,"ARMED %s\n",nonce);
    uint64_t deadline=milliseconds()+15000;
    for(;;){
        if(interrupted||owner_absent()||milliseconds()>=deadline)break;
        pid_t got=waitpid(p,&status,WNOHANG);
        if(got==p){rc=*armed&&WIFEXITED(status)&&WEXITSTATUS(status)==0?0:-1;break;}
        if(got<0&&errno!=EINTR)break;
        if(!*armed){
            ssize_t n=read(request[0],received+have,sizeof received-have);
            if(n>0){
                have+=(size_t)n;
                if(have>(size_t)length||memcmp(received,expected,have))break;
                if(have==(size_t)length){
                    /* This request is sent only after the coordinator validates
                       the pinned generations, platform bytes, nonce and ledger. */
                    long opts=PTRACE_O_EXITKILL|PTRACE_O_TRACEFORK|PTRACE_O_TRACEVFORK|PTRACE_O_TRACECLONE|PTRACE_O_TRACEEXEC|PTRACE_O_TRACEEXIT;
                    if(ptrace(PTRACE_SETOPTIONS,target,0,opts)<0)break;
                    *armed=1;
                    if(ptrace(PTRACE_SETOPTIONS,idle,0,opts)<0||write_ledger("armed"))break;
                    if(write(reply[1],ack,(size_t)acklen)!=acklen)break;
                }
            }else if(n<0&&errno!=EAGAIN&&errno!=EINTR)break;
        }
        /* Sleep on the handshake pipe instead of competing with the guarded
           shell callback via a 1 kHz /proc polling loop on small routers. */
        struct pollfd waiting={request[0],POLLIN|POLLHUP,0};
        if(poll(&waiting,1,100)<0&&errno!=EINTR)break;
    }
done:
    close(request[0]);close(reply[1]);return rc;
}
static int bound_wait_stop(pid_t pid,int *deliver) {
    uint64_t end=milliseconds()+2000;*deliver=0;
    while(milliseconds()<end&&!interrupted){
        int status;pid_t got=waitpid(pid,&status,__WALL|WNOHANG);
        if(got<0){if(errno==EINTR)continue;return -1;}
        if(got==0){struct timespec t={0,1000000};nanosleep(&t,NULL);continue;}
        if(!WIFSTOPPED(status))return -1;
        unsigned event=(unsigned)status>>16;
        if(event==PTRACE_EVENT_STOP&&WSTOPSIG(status)==SIGTRAP)return 0;
        /* A pending external signal must not be swallowed on refusal. */
        if(event==0)*deliver=WSTOPSIG(status);
        return -1;
    }
    return -1;
}
static int bound_pin(pid_t pid,unsigned long long ticks,int *attached,int *deliver) {
    char path[64],line[4096];snprintf(path,sizeof path,"/proc/%d/stat",pid);
    FILE *f=fopen(path,"r");if(!f)return -1;
    char *got=fgets(line,sizeof line,f);fclose(f);if(!got)return -1;
    char *end=strrchr(line,')');if(!end||end[1]!=' '||end[2]=='T'||end[2]=='t')return -1;
    if(!ticks||start_ticks(pid)!=ticks)return -1;
    if(ptrace(PTRACE_SEIZE,pid,0,0)<0)return -1;
    *attached=1;
    if(ptrace(PTRACE_INTERRUPT,pid,0,0)<0||bound_wait_stop(pid,deliver))return -1;
    return start_ticks(pid)==ticks?0:-1;
}
static pid_t bound_single_child(pid_t parent) {
    char path[128],buf[4096],*end;snprintf(path,sizeof path,"/proc/%d/task/%d/children",parent,parent);
    FILE *f=fopen(path,"r");if(!f)return -1;
    size_t n=fread(buf,1,sizeof buf-1,f);int bad=ferror(f);fclose(f);
    if(bad||n==sizeof buf-1)return -1;buf[n]=0;
    errno=0;long p=strtol(buf,&end,10);
    if(errno||end==buf||p<=1||p>INT_MAX)return -1;
    while(*end==' '||*end=='\n'||*end=='\t')end++;
    return *end?-1:(pid_t)p;
}
static void bound_detach(pid_t pid,int attached,int deliver) {
    if(attached) (void)ptrace(PTRACE_DETACH,pid,0,deliver);
    /* If a non-stopped tracee cannot be detached, process exit detaches it.
       EXITKILL has not been enabled in any refusal path. */
}
static int bound_updater_stop_main(int argc,char **argv) {
    /* --stop-updater ASH CONTROL PID START_TICKS TIMEOUT */
    if(argc!=7||argv[2][0]!='/'||argv[3][0]!='/')return 64;
    char *end;errno=0;long parsed=strtol(argv[4],&end,10);
    if(errno||*end||parsed<=1||parsed>INT_MAX)return 64;
    pid_t target=(pid_t)parsed;
    errno=0;unsigned long long ticks=strtoull(argv[5],&end,10);
    if(errno||*end||!ticks||argv[5][0]=='-')return 64;
    errno=0;long seconds=strtol(argv[6],&end,10);
    if(errno||*end||seconds<1||seconds>60)return 64;
    const char *id=getenv("BRORAY_BACKGROUND_OPERATION_ID");
    if(!safe_text(id,96))return 64;strcpy(operation,id);
    protected_route=4;umask(077);
    signal(SIGCHLD,SIG_DFL);signal(SIGTERM,on_signal);signal(SIGINT,on_signal);signal(SIGHUP,on_signal);
    alarm(15);if(register_supervisor(argv[2],argv[3]))return 74;alarm(0);
    int attached=0,child_attached=0,deliver=0,child_deliver=0;pid_t idle=-1;
    /* Waiting for an idle witness is bounded and does not signal the target. */
    for(int attempt=0;attempt<20;attempt++){
        if(interrupted||owner_absent())return end_supervisor(75,"refused",0);
        attached=child_attached=deliver=child_deliver=0;idle=-1;
        if(bound_pin(target,ticks,&attached,&deliver))goto refuse;
        idle=bound_single_child(target);
        if(idle>1){
            unsigned long long child_ticks=start_ticks(idle);
            if(bound_pin(idle,child_ticks,&child_attached,&child_deliver)==0)break;
        }
        bound_detach(idle,child_attached,child_deliver);bound_detach(target,attached,deliver);
        attached=child_attached=0;
        if(attempt==19)return end_supervisor(75,"idle_unconfirmed",0);
        struct timespec t={0,100000000};nanosleep(&t,NULL);
    }
    if(!attached||!child_attached)goto refuse;
    if(add_child(target)||add_child(idle))goto refuse;
    /* Callback checks the canonical bound record, exact /proc identities,
       tracer membership, ledger, idle sleep and both live admission fences. */
    /* One coordinator call holds its guard through verification, native ARM
       handshake and durable authorization. No second full /proc scan needed. */
    int armed=0;
    alarm(15);int authorised=bound_callback(argv[2],argv[3],target,idle,&armed);alarm(0);
    if(authorised||interrupted||owner_absent()){
        if(armed)return end_supervisor(75,"authorization_failed",1);
        goto refuse;
    }
    term_sent=1;
    if(write_ledger("stopping"))return end_supervisor(74,"ledger_failed",1);
    /* The target is still stopped and unreaped: this PID cannot be reused. */
    if(kill(target,SIGTERM)<0)return end_supervisor(74,"term_failed",1);
    if(ptrace(PTRACE_CONT,idle,0,0)<0||ptrace(PTRACE_CONT,target,0,0)<0)
        return end_supervisor(74,"continue_failed",1);
    uint64_t deadline=milliseconds()+(uint64_t)seconds*1000;
    while(count){
        if(interrupted||owner_absent())return end_supervisor(125,"owner_absent",1);
        if(milliseconds()>=deadline)return end_supervisor(124,"deadline",1);
        int status;pid_t pid=waitpid(-1,&status,__WALL|WNOHANG);
        if(pid<0){if(errno==EINTR)continue;return end_supervisor(74,"wait_failed",1);}
        if(pid==0){struct timespec t={0,1000000};nanosleep(&t,NULL);continue;}
        if(WIFEXITED(status)||WIFSIGNALED(status)){
            remove_child(pid);if(write_ledger("draining"))return end_supervisor(74,"ledger_failed",1);continue;
        }
        if(!WIFSTOPPED(status))continue;
        if(index_of(pid)<0&&add_child(pid))return end_supervisor(74,"ledger_failed",1);
        unsigned event=(unsigned)status>>16;int sig=0;
        if(event==PTRACE_EVENT_FORK||event==PTRACE_EVENT_VFORK||event==PTRACE_EVENT_CLONE){
            unsigned long born=0;
            if(ptrace(PTRACE_GETEVENTMSG,pid,0,&born)<0||add_child((pid_t)born))return end_supervisor(74,"child_unconfirmed",1);
        }else if(event==PTRACE_EVENT_EXEC){
            unsigned long former=0;
            if(ptrace(PTRACE_GETEVENTMSG,pid,0,&former)<0)return end_supervisor(74,"exec_unconfirmed",1);
            if(former&&(pid_t)former!=pid)remove_child((pid_t)former);
            if(add_child(pid))return end_supervisor(74,"exec_unconfirmed",1);
        }else if(event==PTRACE_EVENT_STOP&&WSTOPSIG(status)!=SIGTRAP){
            if(ptrace(PTRACE_LISTEN,pid,0,0)<0&&errno!=ESRCH)return end_supervisor(74,"listen_failed",1);
            continue;
        }else if(event==0)sig=WSTOPSIG(status);
        if(ptrace(PTRACE_CONT,pid,0,sig)<0&&errno!=ESRCH)return end_supervisor(74,"continue_failed",1);
    }
    return end_supervisor(0,"complete",0);
refuse:
    bound_detach(idle,child_attached,child_deliver);bound_detach(target,attached,deliver);
    count=0;return end_supervisor(75,"refused",0);
}
