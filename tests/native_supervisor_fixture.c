/* Exercises exec from a non-leader thread and Go-like thread churn. */
#define _GNU_SOURCE
#include <pthread.h>
#include <signal.h>
#include <string.h>
#include <unistd.h>
static void *exec_thread(void *unused){
    (void)unused;
    execl("/bin/sh","sh","-c","printf 'thread-exec-ok'; exit 19",(char *)0);
    _exit(127);
}
static void *empty_thread(void *unused){return unused;}
int main(int argc,char **argv){
    if(argc!=2)return 64;
    pthread_t thread;
    if(!strcmp(argv[1],"thread-exec")){
        if(pthread_create(&thread,0,exec_thread,0))return 1;
        for(;;)pause();
    }
    if(!strcmp(argv[1],"thread-churn")){
        for(int i=0;i<100;i++){
            if(pthread_create(&thread,0,empty_thread,0)||pthread_join(thread,0))return 1;
        }
        return 0;
    }
    if(!strcmp(argv[1],"session-wait")){
        if(setsid()<0)return 1;
        signal(SIGTERM,SIG_IGN);
        for(;;)pause();
    }
    return 64;
}
