/* Legacy bootstrap staging only. Two canonical entry points become refusing
 * guards before reboot. No signal, generation start, activation or STOPPED.
 * Every displaced inode is retained beside its original path; no rename may
 * overwrite a destination. An incomplete staging attempt needs explicit
 * recovery: this entry never guesses/reconstructs missing evidence on retry. */
#ifndef RENAME_NOREPLACE
#define RENAME_NOREPLACE 1
#endif
static const int bg_indices[2]={1,5};
struct bg_input {
    char *intent;size_t size;char live[PATH_MAX];char old_boot[64],operation[97],nonce[65],manifest[65];
    int present[MIGRATION_FILES];unsigned mode[MIGRATION_FILES];
    char before[MIGRATION_FILES][65],after[MIGRATION_FILES][65];
};
static int bg_exists(int fd,const char *name){struct stat s;if(!fstatat(fd,name,&s,AT_SYMLINK_NOFOLLOW))return 1;return errno==ENOENT?0:-1;}
static int bg_exact(int fd,const char *name,const char *bytes,size_t size,unsigned mode){
    struct migration_file f;if(migration_read(fd,name,&f,0))return -1;
    int bad=f.mode!=mode||f.size!=size||memcmp(f.bytes,bytes,size);free(f.bytes);return bad?-1:0;
}
static int bg_record_exact(int fd,const char *name,const char *bytes,size_t size){return bg_exact(fd,name,bytes,size,0600);}
static int bg_source(int fd,const char *mig,const char *live,const char *sha,struct bg_input *b){
    char *copy=NULL;int result=-1;size_t n=0;char digest[65];
    if(migration_names(fd,1)||bg_exists(fd,"boot.receipt")!=0||safe_bytes_at(fd,"intent.record",&b->intent,&b->size))return -1;
    digest_bytes(b->intent,b->size,digest);if(strcmp(digest,sha)||memchr(b->intent,0,b->size))return -1;
    copy=strdup(b->intent);if(!copy)return -1;char *lines[9+MIGRATION_FILES],*cursor=copy;
    while(*cursor&&n<9+MIGRATION_FILES){lines[n++]=cursor;char *end=strchr(cursor,'\n');if(!end)goto done;*end=0;cursor=end+1;}
    if(*cursor||n!=9+MIGRATION_FILES||strcmp(lines[0],"BROray-updater-migration/1")||strcmp(lines[1],mig)||strcmp(lines[2],live)||
       !migration_path(lines[3])||!hex64(lines[4])||!token(lines[5],96)||!token(lines[6],64)||
       (strcmp(lines[7],"running")&&strcmp(lines[7],"stopped"))||strlen(lines[8])!=36||!token(lines[8],36))goto done;
    strcpy(b->live,lines[2]);strcpy(b->old_boot,lines[8]);strcpy(b->operation,lines[5]);strcpy(b->nonce,lines[6]);strcpy(b->manifest,lines[4]);
    struct migration_file manifest_file;if(migration_read(fd,"manifest.record",&manifest_file,0))goto done;
    int bad=strcmp(manifest_file.sha,lines[4]);free(manifest_file.bytes);if(bad)goto done;
    for(int i=0;i<MIGRATION_FILES;i++){
        char path[256];unsigned next;int used=0;
        if(sscanf(lines[9+i],"%255[^\t]\t%d\t%o\t%64[^\t]\t%o\t%64s%n",path,&b->present[i],&b->mode[i],b->before[i],&next,b->after[i],&used)!=6||
           lines[9+i][used]||strcmp(path,migration_paths[i])||next!=0755||!hex64(b->after[i])||
           (b->present[i]!=0&&b->present[i]!=1)||(b->mode[i]&~0777U)||
           (b->present[i]?!hex64(b->before[i]):(b->mode[i]||strcmp(b->before[i],"-"))))goto done;
        char name[32];snprintf(name,sizeof name,"file-%d",i);struct migration_file f;
        if(migration_read(fd,name,&f,0))goto done;bad=f.mode!=0600||strcmp(f.sha,b->after[i]);free(f.bytes);if(bad)goto done;
    }
    char receipt[128];int len=snprintf(receipt,sizeof receipt,"BROray-migration-staged/1\n%s\n",sha);
    if(bg_record_exact(fd,"staged.receipt",receipt,(size_t)len))goto done;
    for(int j=0;j<2;j++)if(!b->present[bg_indices[j]])goto done;
    result=0;
done:free(copy);return result;
}
static int bg_parent(int live,const char *rel,char name[NAME_MAX+1]){
    char copy[PATH_MAX];if(strlen(rel)>=sizeof copy)return -1;strcpy(copy,rel);
    char *slash=strrchr(copy,'/');if(!slash||strlen(slash+1)>NAME_MAX)return -1;strcpy(name,slash+1);*slash=0;
    int fd=openat(live,".",O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);if(fd<0)return -1;char *save=NULL;
    for(char *p=strtok_r(copy,"/",&save);p;p=strtok_r(NULL,"/",&save)){
        if(!strcmp(p,".")||!strcmp(p,"..")){close(fd);return -1;}
        int next=openat(fd,p,O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);close(fd);if(next<0)return -1;fd=next;
        struct stat st;if(fstat(fd,&st)||st.st_uid!=geteuid()||(st.st_mode&0022)){close(fd);return -1;}
    }
    return fd;
}
static int bg_inventory(int live,const struct bg_input *b,unsigned guarded,const char *guard[2],const size_t guard_size[2]){
    for(int i=0;i<MIGRATION_FILES;i++){
        int j=i==1?0:i==5?1:-1;
        if(j>=0&&(guarded&(1U<<j))){if(bg_exact(live,migration_paths[i],guard[j],guard_size[j],0755))return -1;continue;}
        struct migration_file f;if(migration_read(live,migration_paths[i],&f,1))return -1;
        int bad=f.present!=b->present[i]||f.mode!=b->mode[i]||strcmp(f.sha,b->before[i]);free(f.bytes);if(bad)return -1;
    }
    return 0;
}
static int bg_empty(int fd){DIR *d=directory_stream(fd);if(!d)return -1;struct dirent *e;int bad=0;errno=0;while((e=readdir(d)))if(strcmp(e->d_name,".")&&strcmp(e->d_name,"..")){bad=1;break;}if(!e&&errno)bad=1;closedir(d);return bad?-1:0;}
static int bg_names_complete(int fd){
    static const char *names[]={"intent.record","runtime","before-1","before-5","backup-ready.receipt","entry-1.intent","entry-1.done","entry-5.intent","entry-5.done","staged.receipt"};
    DIR *d=directory_stream(fd);if(!d)return -1;struct dirent *e;unsigned seen=0;int bad=0;errno=0;
    while((e=readdir(d))){if(!strcmp(e->d_name,".")||!strcmp(e->d_name,".."))continue;int found=0;
        for(unsigned i=0;i<sizeof names/sizeof names[0];i++)if(!strcmp(e->d_name,names[i])&&!(seen&(1U<<i))){seen|=1U<<i;found=1;break;}
        if(!found){bad=1;break;}errno=0;
    }
    if(!e&&errno)bad=1;closedir(d);return bad||seen!=((1U<<(sizeof names/sizeof names[0]))-1)?-1:0;
}
static int bg_candidate(int fd,const char *name,const char *bytes,size_t n,unsigned mode){
    int f=openat(fd,name,O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW|O_CLOEXEC,0600);if(f<0)return -1;struct stat st;
    int bad=fstat(f,&st)||!S_ISREG(st.st_mode)||st.st_nlink!=1||st.st_uid!=geteuid()||(st.st_mode&07777)!=0600;
    size_t at=0;while(!bad&&at<n){ssize_t x=write(f,bytes+at,n-at);if(x<0&&errno==EINTR)continue;if(x<=0)bad=1;else at+=(size_t)x;}
    if(!bad&&(fsync(f)||fchmod(f,mode)||fsync(f)))bad=1;if(close(f))bad=1;
    if(bad||fsync(fd)||bg_exact(fd,name,bytes,n,mode))return -1;return 0;
}
static int bg_runtime_copy(int base,const char *sha){
    /* Intentionally follow only the kernel's held self executable, as in
     * peer_executable_hash. All output opens remain O_EXCL|O_NOFOLLOW. */
    int fd=open("/proc/self/exe",O_RDONLY|O_CLOEXEC);struct stat st;
    if(fd<0)return -1;
    if(fstat(fd,&st)||!S_ISREG(st.st_mode)||st.st_uid!=geteuid()||st.st_nlink!=1||(st.st_mode&0022)||st.st_size<=0||st.st_size>16777216){close(fd);return -1;}
    size_t size=(size_t)st.st_size,at=0;char *bytes=malloc(size);if(!bytes){close(fd);return -1;}
    int bad=0;while(at<size){ssize_t n=read(fd,bytes+at,size-at);if(n<0&&errno==EINTR)continue;if(n<=0){bad=1;break;}at+=(size_t)n;}
    char digest[65];if(!bad){digest_bytes(bytes,size,digest);bad=strcmp(digest,sha);}
    if(close(fd))bad=1;if(!bad)bad=bg_candidate(base,"runtime",bytes,size,0700);free(bytes);return bad?-1:0;
}
static int bg_runtime_valid(int base,const char *sha){
    struct migration_file f;if(migration_read(base,"runtime",&f,0))return -1;
    int bad=f.mode!=0700||strcmp(f.sha,sha);free(f.bytes);return bad?-1:0;
}
static int bg_move(int fd,const char *from,const char *to){
    /* No fallback to replacing rename or unlink/link. Preserve any unexpected
     * displaced inode and refuse further writes when post-rename proof fails. */
    return syscall(SYS_renameat2,fd,from,fd,to,RENAME_NOREPLACE)||fsync(fd)?-1:0;
}
static int bg_completed_evidence(int base,const int parents[2],char oldname[2][96],char newname[2][96],const struct bg_input *b,const char *intent_sha){
    char proof[192];int pn=snprintf(proof,sizeof proof,"BROray-boot-guard-backup/1\n%s\n",intent_sha);
    if(bg_record_exact(base,"backup-ready.receipt",proof,(size_t)pn))return -1;
    for(int j=0;j<2;j++){
        int i=bg_indices[j];char name[32];struct migration_file saved,old;
        snprintf(name,sizeof name,"before-%d",i);
        if(migration_read(base,name,&saved,0))return -1;
        int bad=saved.mode!=0600||strcmp(saved.sha,b->before[i]);free(saved.bytes);if(bad)return -1;
        if(migration_read(parents[j],oldname[j],&old,0))return -1;
        bad=old.mode!=b->mode[i]||strcmp(old.sha,b->before[i]);free(old.bytes);if(bad)return -1;
        if(bg_exists(parents[j],newname[j])!=0)return -1;
        pn=snprintf(proof,sizeof proof,"BROray-boot-guard-entry/1\n%s\n%d\n",intent_sha,i);
        snprintf(name,sizeof name,"entry-%d.intent",i);if(bg_record_exact(base,name,proof,(size_t)pn))return -1;
        snprintf(name,sizeof name,"entry-%d.done",i);if(bg_record_exact(base,name,proof,(size_t)pn))return -1;
    }
    return 0;
}
static int bg_sync_file(int base,const char *name,unsigned mode){
    int fd=openat(base,name,O_RDONLY|O_NOFOLLOW|O_CLOEXEC|O_NONBLOCK);struct stat held,named;
    if(fd<0)return -1;
    int bad=fstat(fd,&held)||!S_ISREG(held.st_mode)||held.st_uid!=geteuid()||held.st_nlink!=1||(held.st_mode&07777)!=mode||
        fsync(fd)||fstatat(base,name,&named,AT_SYMLINK_NOFOLLOW)||named.st_dev!=held.st_dev||named.st_ino!=held.st_ino||named.st_nlink!=1;
    if(close(fd))bad=1;return bad?-1:0;
}
static int bg_sync_complete(int base,const char *path,const int parents[2],char entry[2][NAME_MAX+1]){
    static const char *names[]={"intent.record","before-1","before-5","backup-ready.receipt","entry-1.intent","entry-1.done","entry-5.intent","entry-5.done","staged.receipt"};
    for(unsigned i=0;i<sizeof names/sizeof names[0];i++)if(bg_sync_file(base,names[i],0600))return -1;
    if(bg_sync_file(base,"runtime",0700)||migration_sync_directory(path,base))return -1;
    for(int j=0;j<2;j++)if(bg_sync_file(parents[j],entry[j],0755)||fsync(parents[j]))return -1;
    return 0;
}
static int bg_binding_text(char record[768],const struct bg_input *input,const char *sha,const char *native){
    int n=snprintf(record,768,"{\"schemaVersion\":1,\"contract\":\"broray-platform-bootguard/1\",\"operationId\":\"%s\",\"stopNonce\":\"%s\",\"migrationIntentSha256\":\"%s\",\"nativeSha256\":\"%s\",\"expectedPlatformManifestSha256\":\"%s\",\"signalsAuthorized\":false,\"activationAllowed\":false}\n",input->operation,input->nonce,sha,native,input->manifest);
    return n>0&&n<768?n:-1;
}
static int bg_bound_exact(int parent,const char *record,size_t n){return parent<0?0:bg_record_exact(parent,"platform-bootguard.json",record,n);}
static void bg_shell_word(FILE *f,const char *word){
    fputc('\'',f);for(;*word;word++){if(*word=='\'')fputs("'\\''",f);else fputc(*word,f);}fputc('\'',f);
}
static int bg_guard_text(int index,const char *migration,const struct bg_input *input,const char *native,char **bytes,size_t *size){
    FILE *g=open_memstream(bytes,size);if(!g)return -1;
    fprintf(g,"#!/opt/bin/ash\n# BROray legacy boot guard/2; entry %d; migration %s\n",index,migration);
    fputs("pending() { printf '%s\\n' 'BRORAY_ERROR:MIGRATION_ACTIVATION_PENDING:Updater migration requires verified boot recovery.' >&2; exit 75; }\n",g);
    if(index==1){
        char runtime[PATH_MAX],path[PATH_MAX+64];const char *prefix=strcmp(input->live,"/")?input->live:"";
        if(snprintf(runtime,sizeof runtime,"%s/opt/var/lib/broray/operations/%s/platform-bootguard/runtime",prefix,input->operation)>=(int)sizeof runtime||
           snprintf(path,sizeof path,"%s/opt/bin:%s/opt/sbin:/usr/bin:/bin:/usr/sbin:/sbin",prefix,prefix)>=(int)sizeof path){fclose(g);return -1;}
        fputs("[ \"${1:-}\" = start ] || pending\nPATH=",g);bg_shell_word(g,path);fputs("\nexport PATH\nLC_ALL=C; export LC_ALL\nruntime=",g);bg_shell_word(g,runtime);
        fputs("\n[ -f \"$runtime\" ] && [ ! -L \"$runtime\" ] && [ -x \"$runtime\" ] || pending\nactual=\"$(sha256sum \"$runtime\")\" || pending\n[ \"${actual%% *}\" = ",g);bg_shell_word(g,native);
        fputs(" ] || pending\n\"$runtime\" recovery-resume ",g);bg_shell_word(g,input->live);fputc(' ',g);bg_shell_word(g,input->operation);fputc(' ',g);bg_shell_word(g,migration);fputc(' ',g);bg_shell_word(g,input->nonce);
        fputs(" && exit 0\npending\n",g);
    }else fputs("pending\n",g);
    return fclose(g)?-1:0;
}
static int bootguard_bind_main(int argc,char **argv){
    /* guard-bind OPERATION_DIR LIVE_ROOT MIGRATION_DIR INTENT_SHA.
     * One fixed evidence record; no platform mutation and no replacing rename.
     * The authenticated coordinator retains its own guard for the whole call. */
    if(argc!=6||!migration_path(argv[2])||!migration_path(argv[3])||!migration_path(argv[4])||!hex64(argv[5]))return 64;
    int base=-1,mig=-1,guards=-1,result=75;struct bg_input input;memset(&input,0,sizeof input);
    char path[PATH_MAX],native[65],current_boot[64],record[768];umask(077);
    if(snprintf(path,sizeof path,"%s/platform-migration",argv[2])>=(int)sizeof path||strcmp(path,argv[4]))goto done;
    base=checked_directory(argv[2]);mig=checked_directory(argv[4]);
    if(base<0||mig<0||flock(base,LOCK_EX|LOCK_NB)||flock(mig,LOCK_SH|LOCK_NB)||
       bg_source(mig,argv[4],argv[3],argv[5],&input)||peer_executable_hash(getpid(),native)||migration_boot(current_boot)||strcmp(current_boot,input.old_boot))goto done;
    const char *op=strrchr(argv[2],'/');if(!op||strcmp(op+1,input.operation))goto done;
    int prior=bg_exists(base,"platform-bootguard.json");if(prior<0)goto done;
    if(!prior){
        if(snprintf(path,sizeof path,"%s/platform-bootguard",argv[2])>=(int)sizeof path)goto done;
        guards=checked_directory(path);
        if(guards<0||flock(guards,LOCK_EX|LOCK_NB)||bg_empty(guards))goto done;
    }
    int n=bg_binding_text(record,&input,argv[5],native);
    if(n<0||migration_record(base,"platform-bootguard.json",record,(size_t)n,!prior))goto done;
    puts("{\"ok\":true,\"bindingReady\":true}");result=0;
done:
    if(guards>=0)close(guards);if(base>=0)close(base);if(mig>=0)close(mig);free(input.intent);
    return result?migration_error("BOOT_GUARD_BINDING_UNCONFIRMED"):0;
}
static int bootguard_main(int argc,char **argv){
    /* guard-stage[-bound]|guard-verify[-bound] GUARD_DIR LIVE_ROOT MIGRATION_DIR INTENT_SHA */
    if(argc!=6||!migration_path(argv[2])||!migration_path(argv[3])||!migration_path(argv[4])||!hex64(argv[5]))return 64;
    int evidence=!strcmp(argv[1],"guard-evidence-bound");
    int verify=evidence||!strcmp(argv[1],"guard-verify")||!strcmp(argv[1],"guard-verify-bound");
    int bound=evidence||!strcmp(argv[1],"guard-stage-bound")||!strcmp(argv[1],"guard-verify-bound");
    int binding_parent=-1,base=-1,live=-1,mig=-1,parents[2]={-1,-1},result=75;
    const char *error="BOOT_GUARD_INPUT_UNCONFIRMED";struct bg_input input;memset(&input,0,sizeof input);
    struct migration_file originals[2];memset(originals,0,sizeof originals);
    char current_boot[64],native_sha[65],*intent=NULL,*guards[2]={NULL,NULL},oldname[2][96],newname[2][96],entry[2][NAME_MAX+1];
    char binding_text[768];size_t binding_size=0,intent_size=0,guard_size[2]={0,0};umask(077);
    base=checked_directory(argv[2]);live=migration_directory(argv[3]);mig=checked_directory(argv[4]);
    if(base<0||live<0||mig<0||flock(base,LOCK_EX|LOCK_NB)||flock(mig,LOCK_SH|LOCK_NB)||migration_boot(current_boot)||peer_executable_hash(getpid(),native_sha))goto done;
    if(bg_source(mig,argv[4],argv[3],argv[5],&input))goto done;
    if(bound){
        char op[PATH_MAX],expected_migration[PATH_MAX];strcpy(op,argv[2]);char *tail=strrchr(op,'/');
        if(!tail||strcmp(tail,"/platform-bootguard"))goto done;*tail=0;
        if(snprintf(expected_migration,sizeof expected_migration,"%s/platform-migration",op)>=(int)sizeof expected_migration||strcmp(expected_migration,argv[4]))goto done;
        tail=strrchr(op,'/');if(!tail||strcmp(tail+1,input.operation))goto done;
        binding_parent=checked_directory(op);int n=bg_binding_text(binding_text,&input,argv[5],native_sha);
        if(binding_parent<0||flock(binding_parent,LOCK_SH|LOCK_NB)||n<0)goto done;binding_size=(size_t)n;
        if(bg_bound_exact(binding_parent,binding_text,binding_size)||bg_sync_file(binding_parent,"platform-bootguard.json",0600)||fsync(binding_parent))goto done;
    }
    int prior=bg_exists(base,"intent.record");if(prior<0)goto done;
    error="BOOT_GUARD_REBOOT_REQUIRES_VERIFICATION";
    if(!verify&&strcmp(current_boot,input.old_boot))goto done;
    error="BOOT_GUARD_EVIDENCE_UNCONFIRMED";
    if(prior?bg_names_complete(base):bg_empty(base))goto done;
    if(verify&&!prior)goto done;
    for(int j=0;j<2;j++){
        int i=bg_indices[j];parents[j]=bg_parent(live,migration_paths[i],entry[j]);if(parents[j]<0)goto done;
        if(bg_guard_text(i,argv[5],&input,native_sha,&guards[j],&guard_size[j]))goto done;
        snprintf(oldname[j],sizeof oldname[j],".broray-bg-%s-%d.previous",argv[5],i);
        snprintf(newname[j],sizeof newname[j],".broray-bg-%s-%d.candidate",argv[5],i);
        if(!prior&&(bg_exists(parents[j],oldname[j])!=0||bg_exists(parents[j],newname[j])!=0))goto done;
    }
    FILE *f=open_memstream(&intent,&intent_size);if(!f)goto done;
    fprintf(f,"BROray-boot-guard-staging/1\n%s\n%s\n%s\n%s\n%s\n%s\n",argv[2],argv[3],argv[4],argv[5],native_sha,input.old_boot);
    for(int j=0;j<2;j++){char h[65];digest_bytes(guards[j],guard_size[j],h);fprintf(f,"%d\t%04o\t%s\t%s\n",bg_indices[j],input.mode[bg_indices[j]],input.before[bg_indices[j]],h);}
    if(fclose(f))goto done;
    char intent_sha[65],receipt[192];digest_bytes(intent,intent_size,intent_sha);
    int receipt_size=snprintf(receipt,sizeof receipt,"BROray-boot-guard-backup/1\n%s\n",intent_sha);
    if(evidence){
        /* Installation has a different live-file inventory. This explicit
         * observational verb verifies only retained staging evidence. It never
         * weakens guard-verify-bound or claims the current paths are guards. */
        if(bg_bound_exact(binding_parent,binding_text,binding_size)||bg_record_exact(base,"intent.record",intent,intent_size)||
           bg_runtime_valid(base,native_sha)||bg_completed_evidence(base,parents,oldname,newname,&input,intent_sha))goto done;
        receipt_size=snprintf(receipt,sizeof receipt,"BROray-boot-guards-staged/1\n%s\n",intent_sha);
        if(bg_record_exact(base,"staged.receipt",receipt,(size_t)receipt_size)||bg_names_complete(base))goto done;
        printf("{\"ok\":true,\"phase\":\"BOOT_GUARD_EVIDENCE_VERIFIED\",\"serviceStopped\":false,\"activationAllowed\":false,\"oldBootId\":\"%s\",\"currentBootId\":\"%s\",\"oldBootEnded\":%s,\"intentSha256\":\"%s\",\"processAuthority\":false,\"platformReady\":false}\n",input.old_boot,current_boot,strcmp(input.old_boot,current_boot)?"true":"false",intent_sha);
        result=0;goto done;
    }
    if(prior){
        if(bg_record_exact(base,"intent.record",intent,intent_size)||bg_record_exact(base,"backup-ready.receipt",receipt,(size_t)receipt_size)||bg_runtime_valid(base,native_sha)||bg_inventory(live,&input,3,(const char **)guards,guard_size))goto done;
    }else{
        if(bg_bound_exact(binding_parent,binding_text,binding_size)||bg_inventory(live,&input,0,(const char **)guards,guard_size))goto done;
        for(int j=0;j<2;j++)if(migration_read(live,migration_paths[bg_indices[j]],&originals[j],0))goto done;
        if(migration_sync_directory(argv[2],base)||migration_record(base,"intent.record",intent,intent_size,1))goto done;
        if(bg_runtime_copy(base,native_sha))goto done;
        for(int j=0;j<2;j++){char name[32];snprintf(name,sizeof name,"before-%d",bg_indices[j]);if(migration_record(base,name,originals[j].bytes,originals[j].size,1))goto done;}
        if(migration_record(base,"backup-ready.receipt",receipt,(size_t)receipt_size,1))goto done;
    }
    for(int j=0;j<2;j++){
        int i=bg_indices[j];char name[32];snprintf(name,sizeof name,"before-%d",i);struct migration_file saved;
        if(migration_read(base,name,&saved,0))goto done;int bad=saved.mode!=0600||strcmp(saved.sha,input.before[i]);free(saved.bytes);if(bad)goto done;
        if(!prior&&bg_candidate(parents[j],newname[j],guards[j],guard_size[j],0755))goto done;
    }
    for(int j=0;j<2;j++){
        int i=bg_indices[j];char start[32],finish[32],proof[192];snprintf(start,sizeof start,"entry-%d.intent",i);snprintf(finish,sizeof finish,"entry-%d.done",i);
        int pn=snprintf(proof,sizeof proof,"BROray-boot-guard-entry/1\n%s\n%d\n",intent_sha,i);
        if(prior){if(bg_record_exact(base,start,proof,(size_t)pn)||bg_record_exact(base,finish,proof,(size_t)pn)||bg_exists(parents[j],newname[j])!=0)goto done;}
        else{
            if(bg_bound_exact(binding_parent,binding_text,binding_size)||bg_record_exact(base,"intent.record",intent,intent_size)||bg_inventory(live,&input,(1U<<j)-1,(const char **)guards,guard_size)||
               bg_exact(parents[j],newname[j],guards[j],guard_size[j],0755)||migration_record(base,start,proof,(size_t)pn,1))goto done;
            if(bg_move(parents[j],entry[j],oldname[j]))goto done;
        }
        struct migration_file old;if(migration_read(parents[j],oldname[j],&old,0))goto done;
        int bad=old.mode!=input.mode[i]||strcmp(old.sha,input.before[i]);free(old.bytes);if(bad)goto done;
        if(!prior){
            if(bg_bound_exact(binding_parent,binding_text,binding_size)||bg_record_exact(base,"intent.record",intent,intent_size)||bg_exact(parents[j],newname[j],guards[j],guard_size[j],0755)||
               bg_move(parents[j],newname[j],entry[j])||bg_bound_exact(binding_parent,binding_text,binding_size)||bg_inventory(live,&input,(1U<<(j+1))-1,(const char **)guards,guard_size)||
               migration_record(base,finish,proof,(size_t)pn,1))goto done;
        }
    }
    /* The first displaced inode may still be held by a legacy writer while
     * the second entry is installed. Validate the entire evidence set again;
     * staging remains only a snapshot, never proof that legacy writers ended. */
    if(bg_bound_exact(binding_parent,binding_text,binding_size)||bg_record_exact(base,"intent.record",intent,intent_size)||bg_runtime_valid(base,native_sha)||bg_inventory(live,&input,3,(const char **)guards,guard_size)||
       bg_completed_evidence(base,parents,oldname,newname,&input,intent_sha))goto done;
    receipt_size=snprintf(receipt,sizeof receipt,"BROray-boot-guards-staged/1\n%s\n",intent_sha);
    if(prior?bg_record_exact(base,"staged.receipt",receipt,(size_t)receipt_size):migration_record(base,"staged.receipt",receipt,(size_t)receipt_size,1))goto done;
    /* A completed-looking replay may be a lost response at the last fsync.
     * Confirm durable own files and directory entries, then revalidate bytes. */
    if(bg_names_complete(base)||bg_sync_complete(base,argv[2],parents,entry)||bg_bound_exact(binding_parent,binding_text,binding_size)||bg_record_exact(base,"intent.record",intent,intent_size)||bg_runtime_valid(base,native_sha)||
       bg_inventory(live,&input,3,(const char **)guards,guard_size)||bg_completed_evidence(base,parents,oldname,newname,&input,intent_sha))goto done;
    printf("{\"ok\":true,\"phase\":\"%s\",\"serviceStopped\":false,\"activationAllowed\":false,\"oldBootId\":\"%s\",\"currentBootId\":\"%s\",\"oldBootEnded\":%s,\"intentSha256\":\"%s\"}\n",verify?"BOOT_GUARDS_VERIFIED":"BOOT_GUARDS_STAGED",input.old_boot,current_boot,strcmp(input.old_boot,current_boot)?"true":"false",intent_sha);result=0;
done:
    for(int j=0;j<2;j++){if(parents[j]>=0)close(parents[j]);free(guards[j]);free(originals[j].bytes);}
    if(binding_parent>=0)close(binding_parent);if(base>=0)close(base);if(live>=0)close(live);if(mig>=0)close(mig);free(input.intent);free(intent);
    return result?migration_error(error):0;
}

/* Native runtime retention is independent of prunable operation history.
 * The canonical caller owns STORE and supplies the authenticated native hash.
 * A partial/unknown entry is evidence, never a cache to reconstruct or replace.
 * This verb cannot launch, stop or authorize any process. */
static int runtime_names(int base){
    DIR *dir=directory_stream(base);if(!dir)return -1;
    struct dirent *entry;unsigned seen=0;int bad=0;errno=0;
    while((entry=readdir(dir))){
        const char *name=entry->d_name;if(!strcmp(name,".")||!strcmp(name,".."))continue;
        unsigned bit=!strcmp(name,"runtime")?1U:!strcmp(name,"identity.json")?2U:0;
        if(!bit||(seen&bit)){bad=1;break;}seen|=bit;errno=0;
    }
    if(!entry&&errno)bad=1;closedir(dir);return bad||seen!=3?-1:0;
}
static int runtime_retain_main(int argc,char **argv){
    /* runtime-retain|runtime-verify STORE EXPECTED_NATIVE_SHA */
    if(argc!=4||!migration_path(argv[2])||!hex64(argv[3]))return 64;
    int verify=!strcmp(argv[1],"runtime-verify"),root=-1,base=-1,result=75;
    char own[65],path[PATH_MAX],record[256];umask(077);
    if(peer_executable_hash(getpid(),own)||strcmp(own,argv[3]))goto done;
    if(snprintf(path,sizeof path,"%s/%s",argv[2],own)>=(int)sizeof path)goto done;
    root=checked_directory(argv[2]);
    if(root<0||flock(root,LOCK_EX|LOCK_NB)||migration_sync_directory(argv[2],root))goto done;
    int prior=bg_exists(root,own);if(prior<0)goto done;
    if(verify&&!prior)goto done;
    if(!prior&&(mkdirat(root,own,0700)||fsync(root)))goto done;
    base=checked_directory(path);if(base<0||flock(base,LOCK_EX|LOCK_NB))goto done;
    if(prior?runtime_names(base):bg_empty(base))goto done;
    int n=snprintf(record,sizeof record,"{\"schemaVersion\":1,\"contract\":\"broray-updater-runtime/1\",\"runtimeSha256\":\"%s\",\"mode\":448,\"processAuthority\":false}\n",own);
    if(n<0||n>=(int)sizeof record)goto done;
    if(!prior&&bg_runtime_copy(base,own))goto done;
    if(bg_runtime_valid(base,own)||migration_record(base,"identity.json",record,(size_t)n,!prior)||runtime_names(base)||
       bg_sync_file(base,"runtime",0700)||bg_sync_file(base,"identity.json",0600)||migration_sync_directory(path,base)||
       bg_runtime_valid(base,own)||bg_record_exact(base,"identity.json",record,(size_t)n)||runtime_names(base))goto done;
    printf("{\"ok\":true,\"runtimeSha256\":\"%s\",\"runtimePath\":",own);
    char runtime[PATH_MAX];if(snprintf(runtime,sizeof runtime,"%s/runtime",path)>=(int)sizeof runtime)goto done;
    json_string(stdout,runtime);puts(",\"processAuthority\":false}");result=0;
done:
    if(base>=0)close(base);if(root>=0)close(root);
    return result?migration_error("UPDATER_RUNTIME_RETENTION_UNCONFIRMED"):0;
}
