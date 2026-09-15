/* Physical primitive gate for the proposed operation supervisor, not product.
 * Every process is created by this test. No PID is read from router state.
 */
#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <pthread.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/prctl.h>
#include <sys/ptrace.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>

static const long opts=PTRACE_O_EXITKILL|PTRACE_O_TRACEFORK|
    PTRACE_O_TRACEVFORK|PTRACE_O_TRACECLONE|PTRACE_O_TRACEEXEC;
struct evidence {int count,forks,clones,execs;pid_t pids[64];};
static void pause_ms(int ms){struct timespec t={ms/1000,(ms%1000)*1000000L};nanosleep(&t,0);}
static void *thread_work(void *unused){(void)unused;for(;;)pause();return 0;}
static void add(struct evidence *e,pid_t pid){
    for(int i=0;i<e->count;i++)if(e->pids[i]==pid)return;
    if(e->count==64)_exit(50);
    e->pids[e->count++]=pid;
}
static pid_t gated(int *write_fd){
    int p[2];if(pipe(p))return -1;
    pid_t child=fork();if(child<0)return -1;
    if(child==0){close(p[1]);char c;read(p[0],&c,1);_exit(0);}
    close(p[0]);*write_fd=p[1];return child;
}
static int worker(int ready){
    pid_t escaped=fork();if(escaped<0)return 51;
    if(escaped==0){
        if(setsid()<0)_exit(52);
        char script[128];snprintf(script,sizeof script,"trap '' TERM; sleep 60 & echo LEAF >&%d; wait",ready);
        execl("/opt/bin/ash","ash","-c",script,(char *)0);_exit(53);
    }
    pthread_t thread;if(pthread_create(&thread,0,thread_work,0))return 54;
    if(write(ready,"ROOT\n",5)!=5)return 55;
    for(;;)pause();
}
static void tracer(const char *self,int report,int stop){
    int gate[2],ready[2];if(pipe(gate)||pipe(ready))_exit(60);
    pid_t root=fork();if(root<0)_exit(61);
    if(root==0){
        close(gate[1]);close(ready[0]);char c;
        if(read(gate[0],&c,1)!=1)_exit(62);
        close(gate[0]);char fd[20];snprintf(fd,sizeof fd,"%d",ready[1]);
        execl(self,self,"--worker",fd,(char *)0);_exit(63);
    }
    close(gate[0]);close(ready[1]);
    if(ptrace(PTRACE_SEIZE,root,0,opts)<0)_exit(64);
    if(write(gate[1],"G",1)!=1)_exit(65);close(gate[1]);
    fcntl(ready[0],F_SETFL,O_NONBLOCK);
    struct evidence e={0};add(&e,root);
    char messages[128]={0};size_t length=0;
    for(int iteration=0;iteration<1500;iteration++){
        int status;pid_t pid=waitpid(-1,&status,__WALL|WNOHANG);
        if(pid<0)_exit(66);
        if(pid>0){
            if(!WIFSTOPPED(status))_exit(67);
            unsigned event=(unsigned)status>>16;int sig=0;
            if(event==PTRACE_EVENT_FORK||event==PTRACE_EVENT_VFORK||event==PTRACE_EVENT_CLONE){
                unsigned long child=0;if(ptrace(PTRACE_GETEVENTMSG,pid,0,&child)<0)_exit(68);
                add(&e,(pid_t)child);
                if(event==PTRACE_EVENT_CLONE)e.clones++;else e.forks++;
            }else if(event==PTRACE_EVENT_EXEC){e.execs++;}
            else if(event==0){sig=WSTOPSIG(status);}
            if(ptrace(PTRACE_CONT,pid,0,sig)<0)_exit(69);
        }
        ssize_t n=read(ready[0],messages+length,sizeof messages-length-1);
        if(n>0){length+=(size_t)n;messages[length]=0;}
        if(e.forks>=2&&e.clones>=1&&e.execs>=3&&strstr(messages,"ROOT")&&strstr(messages,"LEAF")){
            if(write(report,&e,sizeof e)!=sizeof e)_exit(70);
            char c;if(read(stop,&c,1)!=1)_exit(71);
            raise(SIGKILL);_exit(72);
        }
        if(pid==0)pause_ms(5);
    }
    _exit(73);
}
int main(int argc,char **argv){
    if(argc==3&&strcmp(argv[1],"--worker")==0)return worker(atoi(argv[2]));
    alarm(20);setvbuf(stdout,0,_IONBF,0);
    if(prctl(PR_SET_CHILD_SUBREAPER,1,0,0,0))return 1;
    int gate,status;pid_t child=gated(&gate);if(child<0)return 2;
    if(ptrace(PTRACE_SEIZE,child,0,opts)<0){close(gate);waitpid(child,&status,0);return 3;}
    if(ptrace(PTRACE_INTERRUPT,child,0,0)<0)return 4;
    if(waitpid(child,&status,0)!=child||!WIFSTOPPED(status))return 5;
    /* Stable kernel ownership: our unreaped fork child at a ptrace stop. */
    if(kill(child,SIGTERM)<0||ptrace(PTRACE_CONT,child,0,0)<0)return 6;
    if(waitpid(child,&status,0)!=child||!WIFSTOPPED(status)||WSTOPSIG(status)!=SIGTERM)return 7;
    if(ptrace(PTRACE_CONT,child,0,SIGTERM)<0)return 8;
    if(waitpid(child,&status,0)!=child||!WIFSIGNALED(status)||WTERMSIG(status)!=SIGTERM)return 9;
    close(gate);puts("PASS owned_stopped_child_term");

    int sentinel_gate;pid_t sentinel=gated(&sentinel_gate);if(sentinel<0)return 10;
    int report[2],stop[2];if(pipe(report)||pipe(stop))return 11;
    pid_t supervisor=fork();if(supervisor<0)return 12;
    if(supervisor==0){close(report[0]);close(stop[1]);tracer(argv[0],report[1],stop[0]);_exit(74);}
    close(report[1]);close(stop[0]);struct evidence e;
    if(read(report[0],&e,sizeof e)!=sizeof e)return 13;
    if(e.count<4||e.count>64)return 14;
    close(report[0]);
    for(int i=0;i<e.count;i++)if(e.pids[i]==sentinel)return 15;
    if(write(stop[1],"K",1)!=1)return 16;close(stop[1]);
    if(waitpid(supervisor,&status,0)!=supervisor||!WIFSIGNALED(status)||WTERMSIG(status)!=SIGKILL)return 17;
    int gone=0;
    for(int attempt=0;attempt<500;attempt++){
        for(;;){pid_t p=waitpid(-1,&status,__WALL|WNOHANG);if(p<=0)break;if(p==sentinel)return 18;}
        gone=1;
        for(int i=0;i<e.count;i++){
            char path[64];snprintf(path,sizeof path,"/proc/%d",e.pids[i]);
            if(access(path,F_OK)==0)gone=0;
        }
        if(gone)break;pause_ms(10);
    }
    if(!gone)return 19;
    if(kill(sentinel,0)<0)return 20;
    if(write(sentinel_gate,"E",1)!=1)return 21;close(sentinel_gate);
    if(waitpid(sentinel,&status,0)!=sentinel||!WIFEXITED(status)||WEXITSTATUS(status)!=0)return 22;
    puts("PASS tracer_death_kills_forked_cloned_execed_and_session_escaped_descendants");
    puts("PASS unrelated_sentinel_preserved");
    printf("{\"status\":\"PASS\",\"tracees\":%d,\"forkEvents\":%d,\"cloneEvents\":%d,\"execEvents\":%d,\"checks\":3,\"productRunnerImplemented\":false}\n",e.count,e.forks,e.clones,e.execs);
    return 0;
}
