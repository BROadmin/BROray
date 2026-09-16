/* Short-lived supervisor for cooperative helpers and protected route jobs.
 * Protected jobs ignore user cancellation; owner loss and internal deadlines
 * still close their execution gate and terminate the traced process tree.
 * The direct fork-child
 * is never reaped before TERM. Other tracees receive no userspace PID signals:
 * the kernel's PTRACE_O_EXITKILL owns their termination when this process exits.
 * Every tracee is recorded in RAM before it is allowed to execute user code.
 */
#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <signal.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/ptrace.h>
#include <sys/stat.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>

#define MAX_CHILDREN 256
struct child {pid_t pid;unsigned long long ticks;};
static struct child children[MAX_CHILDREN];
static int count=0,term_sent=0,kill_triggered=0,protected_route=0;
static unsigned long revision=0;
static char ledger[PATH_MAX],boot[128],nonce[33],operation[97];
static pid_t owner_pid,root_pid;
static unsigned long long owner_ticks,supervisor_ticks;
static volatile sig_atomic_t interrupted=0;
static void on_signal(int sig){(void)sig;interrupted=1;}
static uint64_t milliseconds(void){struct timespec t;if(clock_gettime(CLOCK_MONOTONIC,&t))_exit(74);return (uint64_t)t.tv_sec*1000+t.tv_nsec/1000000;}
static void pause_ms(int ms){struct timespec t={ms/1000,(ms%1000)*1000000L};nanosleep(&t,0);}
static int safe_text(const char *s,size_t max){
    if(!s||!s[0]||strlen(s)>max)return 0;
    for(const unsigned char *p=(const unsigned char *)s;*p;p++)
        if(!((*p>='0'&&*p<='9')||(*p>='a'&&*p<='z')||(*p>='A'&&*p<='Z')||*p=='-'||*p=='_'||*p=='.'))return 0;
    return 1;
}
static unsigned long long start_ticks(pid_t pid){
    char path[64],line[4096];snprintf(path,sizeof path,"/proc/%d/stat",pid);
    FILE *f=fopen(path,"r");if(!f)return 0;
    char *got=fgets(line,sizeof line,f);fclose(f);if(!got)return 0;
    char *p=strrchr(line,')');if(!p||p[1]!=' ')return 0;p+=2;
    for(int i=1;i<20;i++){p=strchr(p,' ');if(!p)return 0;p++;}
    char *end;unsigned long long n=strtoull(p,&end,10);
    return end==p||(*end!=' '&&*end!='\n')?0:n;
}
static int write_ledger(const char *state){
    char temporary[PATH_MAX];struct stat st;
    if(snprintf(temporary,sizeof temporary,"%s.tmp.%d",ledger,getpid())>=(int)sizeof temporary)return -1;
    if(lstat(ledger,&st)==0&&(!S_ISREG(st.st_mode)||st.st_uid!=geteuid()||st.st_nlink!=1))return -1;
    int fd=open(temporary,O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW,0600);if(fd<0)return -1;
    FILE *f=fdopen(fd,"w");if(!f){close(fd);return -1;}
    int rc=fprintf(f,"{\"schemaVersion\":1,\"operationId\":\"%s\",\"supervisorId\":\"%s\",\"supervisorPid\":%d,\"supervisorStartTicks\":\"%llu\",\"bootId\":\"%s\",\"revision\":%lu,\"state\":\"%s\",\"termSent\":%s,\"killTriggered\":%s,\"children\":[",
        operation,nonce,getpid(),supervisor_ticks,boot,++revision,state,term_sent?"true":"false",kill_triggered?"true":"false");
    for(int i=0;i<count&&rc>=0;i++)rc=fprintf(f,"%s{\"pid\":%d,\"startTicks\":\"%llu\",\"bootId\":\"%s\"}",i?",":"",children[i].pid,children[i].ticks,boot);
    if(rc>=0)rc=fprintf(f,"]}\n");
    if(fclose(f)||rc<0){unlink(temporary);return -1;}
    if(rename(temporary,ledger)){unlink(temporary);return -1;}
    return 0;
}
static int index_of(pid_t pid){for(int i=0;i<count;i++)if(children[i].pid==pid)return i;return -1;}
static void remove_child(pid_t pid){int i=index_of(pid);if(i>=0)children[i]=children[--count];}
static int add_child(pid_t pid){
    int i=index_of(pid);unsigned long long ticks=start_ticks(pid);if(!ticks)return -1;
    if(i<0){if(count==MAX_CHILDREN)return -1;i=count++;}
    children[i].pid=pid;children[i].ticks=ticks;return write_ledger("running");
}
static int register_supervisor(const char *ash,const char *control){
    unsigned char bytes[16];int random=open("/dev/urandom",O_RDONLY);if(random<0)return -1;
    ssize_t got=read(random,bytes,sizeof bytes);close(random);if(got!=sizeof bytes)return -1;
    for(int i=0;i<16;i++)snprintf(nonce+i*2,3,"%02x",bytes[i]);
    int output[2];if(pipe(output))return -1;
    pid_t callback=fork();if(callback<0)return -1;
    if(callback==0){
        close(output[0]);dup2(output[1],STDOUT_FILENO);close(output[1]);
        int null=open("/dev/null",O_RDONLY);if(null>=0){dup2(null,STDIN_FILENO);close(null);}
        char pid[24];snprintf(pid,sizeof pid,"%d",getppid());
        execl(ash,ash,control,protected_route?"register-route":"register",pid,nonce,(char *)0);_exit(74);
    }
    close(output[1]);char buffer[PATH_MAX+512];size_t length=0;
    for(;;){ssize_t n=read(output[0],buffer+length,sizeof buffer-length-1);if(n<0&&errno==EINTR)continue;if(n<=0)break;length+=(size_t)n;if(length==sizeof buffer-1)break;}
    close(output[0]);int status;while(waitpid(callback,&status,0)<0){if(errno!=EINTR)return -1;}
    if(!WIFEXITED(status)||WEXITSTATUS(status)!=0)return -1;
    buffer[length]=0;char *fields[4],*p=buffer;
    for(int i=0;i<4;i++){fields[i]=p;char *end=strchr(p,i==3?'\n':'\t');if(!end)return -1;*end=0;p=end+1;}
    char *end;long pid=strtol(fields[0],&end,10);if(*end||pid<=1||pid>INT_MAX)return -1;owner_pid=(pid_t)pid;
    owner_ticks=strtoull(fields[1],&end,10);if(*end||!owner_ticks)return -1;
    if(!safe_text(fields[2],sizeof boot-1)||fields[3][0]!='/'||strlen(fields[3])>=sizeof ledger)return -1;
    strcpy(boot,fields[2]);strcpy(ledger,fields[3]);
    supervisor_ticks=start_ticks(getpid());if(!supervisor_ticks)return -1;
    return write_ledger("gated");
}
static int owner_absent(void){
    unsigned long long current=start_ticks(owner_pid);
    if(current)return current!=owner_ticks;
    char path[64];snprintf(path,sizeof path,"/proc/%d",owner_pid);
    return access(path,F_OK)<0&&errno==ENOENT;
}
static int cancellation(const char *file){struct stat st;return lstat(file,&st)==0;}
static int end_supervisor(int result,const char *state,int killing){
    if(killing)kill_triggered=1;
    write_ledger(state);
    /* Exiting activates EXITKILL for every surviving tracee, including escaped
       sessions and threads. The coordinator still verifies their disappearance. */
    return result;
}
int main(int argc,char **argv){
    if(argc==2&&!strcmp(argv[1],"--version")){puts("broray-ops-supervisor/2 ptrace-exitkill cooperative-helper protected-route");return 0;}
    if(argc>1&&!strcmp(argv[1],"--protected-route")){protected_route=1;argc--;argv++;}
    /* ash, control script, cancel-file, timeout seconds, cooperative grace,
       TERM grace, --, absolute command, command arguments */
    if(argc<9||strcmp(argv[7],"--")||argv[1][0]!='/'||argv[2][0]!='/'||argv[3][0]!='/'||argv[8][0]!='/')return 64;
    const char *id=getenv("BRORAY_BACKGROUND_OPERATION_ID");if(!safe_text(id,96))return 64;strcpy(operation,id);
    int durations[3];for(int i=0;i<3;i++){char *end;long n=strtol(argv[4+i],&end,10);if(*end||n<0||n>3600)return 64;durations[i]=(int)n;}
    if(!durations[0]||durations[1]>30||durations[2]>30)return 64;
    umask(077);signal(SIGCHLD,SIG_DFL);signal(SIGTERM,on_signal);signal(SIGINT,on_signal);signal(SIGHUP,on_signal);
    alarm(15);
    if(register_supervisor(argv[1],argv[2]))return 74;
    alarm(0);
    int gate[2];if(pipe(gate))return end_supervisor(74,"failed",0);
    root_pid=fork();if(root_pid<0)return end_supervisor(74,"failed",0);
    if(root_pid==0){
        close(gate[1]);char c;if(read(gate[0],&c,1)!=1)_exit(74);close(gate[0]);
        signal(SIGTERM,SIG_DFL);signal(SIGINT,SIG_DFL);signal(SIGHUP,SIG_DFL);
        setenv("BRORAY_OPS_SUPERVISED","ptrace/1",1);
        if(protected_route)setenv("BRORAY_OPS_ROUTE_SUPERVISED","ptrace/1",1);
        else unsetenv("BRORAY_OPS_ROUTE_SUPERVISED");
        execv(argv[8],argv+8);_exit(127);
    }
    close(gate[0]);
    long opts=PTRACE_O_EXITKILL|PTRACE_O_TRACEFORK|PTRACE_O_TRACEVFORK|PTRACE_O_TRACECLONE|PTRACE_O_TRACEEXEC|PTRACE_O_TRACEEXIT;
    if(ptrace(PTRACE_SEIZE,root_pid,0,opts)<0){close(gate[1]);int status;waitpid(root_pid,&status,0);return end_supervisor(74,"unsupported",0);}
    if(add_child(root_pid)){close(gate[1]);return end_supervisor(74,"ledger_failed",1);}
    if(interrupted||(!protected_route&&cancellation(argv[3]))||owner_absent()){close(gate[1]);return end_supervisor(130,"cancelled_before_gate",1);}
    if(write(gate[1],"G",1)!=1){close(gate[1]);return end_supervisor(74,"gate_failed",1);}close(gate[1]);
    uint64_t deadline=milliseconds()+(uint64_t)durations[0]*1000,stop_at=0,term_at=0;
    int reason=0;
    for(;;){
        uint64_t now=milliseconds();
        if(owner_absent())return end_supervisor(125,"owner_absent",1);
        if(!reason&&(interrupted||(!protected_route&&cancellation(argv[3]))||now>=deadline)){reason=now>=deadline?124:130;stop_at=now;}
        if(reason&&!term_sent&&now-stop_at>=(uint64_t)(reason==124?0:durations[1])*1000){
            /* Direct fork-child, never reaped at this point. Even if already
               exited, its PID cannot belong to an unrelated process. */
            if(kill(root_pid,SIGTERM)<0&&errno!=ESRCH)return end_supervisor(74,"term_failed",1);
            term_sent=1;term_at=now;if(write_ledger("stopping"))return end_supervisor(74,"ledger_failed",1);
        }
        if(term_sent&&now-term_at>=(uint64_t)durations[2]*1000)return end_supervisor(reason,"killing",1);
        int status;pid_t pid=waitpid(-1,&status,__WALL|WNOHANG);
        if(pid<0){if(errno==EINTR)continue;return end_supervisor(74,"wait_failed",1);}
        if(pid==0){pause_ms(10);continue;}
        if(WIFEXITED(status)||WIFSIGNALED(status)){
            remove_child(pid);
            if(pid==root_pid){int rc=WIFEXITED(status)?WEXITSTATUS(status):128+WTERMSIG(status);return end_supervisor(reason?reason:rc,count?"draining":"complete",count!=0);}
            if(write_ledger("running"))return end_supervisor(74,"ledger_failed",1);
            continue;
        }
        if(!WIFSTOPPED(status))continue;
        if(index_of(pid)<0&&add_child(pid))return end_supervisor(74,"ledger_failed",1);
        unsigned event=(unsigned)status>>16;int deliver=0;
        if(event==PTRACE_EVENT_FORK||event==PTRACE_EVENT_VFORK||event==PTRACE_EVENT_CLONE){
            unsigned long born=0;if(ptrace(PTRACE_GETEVENTMSG,pid,0,&born)<0||add_child((pid_t)born))return end_supervisor(74,"child_unconfirmed",1);
        }else if(event==PTRACE_EVENT_EXEC){
            unsigned long former=0;if(ptrace(PTRACE_GETEVENTMSG,pid,0,&former)<0)return end_supervisor(74,"exec_unconfirmed",1);
            if(former&&(pid_t)former!=pid)remove_child((pid_t)former);
            if(add_child(pid))return end_supervisor(74,"exec_unconfirmed",1);
        }else if(event==PTRACE_EVENT_STOP&&WSTOPSIG(status)!=SIGTRAP){
            if(ptrace(PTRACE_LISTEN,pid,0,0)<0&&errno!=ESRCH)return end_supervisor(74,"listen_failed",1);
            continue;
        }else if(event==0){deliver=WSTOPSIG(status);}
        if(ptrace(PTRACE_CONT,pid,0,deliver)<0&&errno!=ESRCH)return end_supervisor(74,"continue_failed",1);
    }
}
