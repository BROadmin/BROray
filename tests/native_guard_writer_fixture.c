/* Isolated regression fixture. Only signals itself; child exits after 20 s. */
#define _POSIX_C_SOURCE 200809L
#include <fcntl.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <sys/types.h>
#include <time.h>
#include <unistd.h>

int main(int argc,char **argv) {
    if(argc!=7)return 64;
    int ready_pipe[2];if(pipe(ready_pipe))return 74;
    pid_t child=fork();if(child<0)return 74;
    if(child==0) {
        close(ready_pipe[0]);
        int nullfd=open("/dev/null",O_RDWR);if(nullfd<0)_exit(74);
        for(int i=0;i<3;i++)if(dup2(nullfd,i)<0)_exit(74);
        if(nullfd>2)close(nullfd);
        if(setsid()<0)_exit(74);
        FILE *ready=fopen(argv[2],"wx");if(!ready)_exit(74);
        fprintf(ready,"%ld\n",(long)getpid());if(fclose(ready))_exit(74);
        if(write(ready_pipe[1],"R",1)!=1)_exit(74);
        close(ready_pipe[1]);
        struct timespec pause={0,10000000};
        for(int n=0;n<2000;n++) {
            if(access(argv[3],F_OK)==0) {
                char *command[]={argv[1],argv[4],argv[5],argv[6],NULL};
                execv(argv[1],command);_exit(74);
            }
            nanosleep(&pause,NULL);
        }
        _exit(124);
    }
    close(ready_pipe[1]);char byte=0;
    if(read(ready_pipe[0],&byte,1)!=1 || byte!='R')return 74;
    close(ready_pipe[0]);
    kill(getpid(),SIGKILL);
    return 74;
}
