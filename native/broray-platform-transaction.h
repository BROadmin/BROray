/* Bounded protected platform writer. No helper performs mutations, no service
 * starts here. Immutable backup evidence is verified under the same existing
 * installation guard as legacy STOPPED. Partial/unknown evidence stays intact. */
static int pt_inventory(int live,const struct migration_file before[MIGRATION_FILES]){
    for(int i=0;i<MIGRATION_FILES;i++){
        struct migration_file now;if(migration_read(live,migration_paths[i],&now,1))return -1;
        int bad=now.present!=before[i].present||now.mode!=before[i].mode||strcmp(now.sha,before[i].sha);
        free(now.bytes);if(bad)return -1;
    }
    return 0;
}
static int pt_backup_names(int base,const struct migration_file before[MIGRATION_FILES]){
    DIR *d=directory_stream(base);if(!d)return -1;struct dirent *e;unsigned seen=0,wanted=15U;int bad=0;errno=0;
    for(int i=0;i<MIGRATION_FILES;i++)if(before[i].present)wanted|=1U<<(i+4);
    while((e=readdir(d))){const char *name=e->d_name;if(!strcmp(name,".")||!strcmp(name,".."))continue;
        unsigned bit=!strcmp(name,"intent.json")?1U:!strcmp(name,"executor.record")?2U:!strcmp(name,"ready.anchor")?4U:!strcmp(name,"ready.receipt")?8U:0U;
        for(int i=0;i<MIGRATION_FILES;i++){char n[32];snprintf(n,sizeof n,"before-%d",i);if(!strcmp(name,n))bit=1U<<(i+4);}
        if(!bit||!(wanted&bit)||(seen&bit)){bad=1;break;}seen|=bit;errno=0;
    }
    if(!e&&errno)bad=1;closedir(d);return bad||seen!=wanted?-1:0;
}
static int pt_backup_copies(int base,const struct migration_file before[MIGRATION_FILES]){
    for(int i=0;i<MIGRATION_FILES;i++){
        char name[32];snprintf(name,sizeof name,"before-%d",i);
        if(before[i].present){if(bg_record_exact(base,name,before[i].bytes,before[i].size))return -1;}
        else if(bg_exists(base,name)!=0)return -1;
    }
    return 0;
}
static int pt_backup_intent(const struct bg_input *input,const char *migration,const char *native,int was_running,const char *stopped,const char *execution,const struct migration_file before[MIGRATION_FILES],char **intent,size_t *size){
    FILE *f=open_memstream(intent,size);if(!f)return -1;
    fprintf(f,"{\"schemaVersion\":1,\"contract\":\"broray-platform-backup/1\",\"operationId\":\"%s\",\"stopNonce\":\"%s\",\"oldBootId\":\"%s\",\"oldGenerationId\":\"legacy@%s\",\"oldServiceWasRunning\":%s,\"migrationIntentSha256\":\"%s\",\"expectedPlatformManifestSha256\":\"%s\",\"nativeSha256\":\"%s\",\"stoppedReceiptSha256\":\"%s\",\"executorRecordSha256\":\"%s\",\"before\":[",input->operation,input->nonce,input->old_boot,input->old_boot,was_running?"true":"false",migration,input->manifest,native,stopped,execution);
    for(int i=0;i<MIGRATION_FILES;i++){
        if(i)fputc(',',f);fprintf(f,"{\"path\":\"%s\",\"present\":%s,\"mode\":%u,\"sha256\":",migration_paths[i],before[i].present?"true":"false",before[i].mode);
        if(before[i].present)json_string(f,before[i].sha);else fputs("null",f);fputc('}',f);
    }
    fputs("]}\n",f);return fclose(f)?-1:0;
}
static int platform_backup_apply(char **argv,int op,const char *op_path,const struct bg_input *input,const char *native,const struct identity *executor,int held,int statefd,const char *shell,const char *controller){
    int live=-1,base=-1,result=75,replay=0;char path[PATH_MAX],prefix[512],binding[512],intent_sha[65],execution_sha[65];
    char terminal[192],anchor_record[128],terminal_sha[65];
    struct migration_file before[MIGRATION_FILES],stopped,original,stored;memset(before,0,sizeof before);memset(&stopped,0,sizeof stopped);memset(&original,0,sizeof original);memset(&stored,0,sizeof stored);
    char *execution=NULL,*intent=NULL;size_t execution_size=0,intent_size=0;
    /* Verification mode cannot create/complete a retirement. The caller has
     * just verified the complete original context; repeat it before/after READY. */
    if(legacy_retirement_apply(argv,op,op_path,input,native,executor,held,statefd,shell,controller,1))goto done;
    if(migration_read(op,"platform-legacy-retirement/stopped.receipt",&stopped,0)||stopped.mode!=0600||
       migration_read(op,"platform-legacy-control/snapshot.json",&original,0)||original.mode!=0600)goto done;
    int was_running=strstr(original.bytes,"\"oldServiceWasRunning\":true,")!=NULL;
    if(!was_running&&!strstr(original.bytes,"\"oldServiceWasRunning\":false,"))goto done;
    live=migration_directory(argv[2]);if(live<0)goto done;
    for(int i=0;i<MIGRATION_FILES;i++)if(migration_read(live,migration_paths[i],&before[i],1))goto done;
    int directory=bg_exists(op,"platform-backup"),bound=bg_exists(op,"platform-backup.json");
    if(directory<0||bound<0||directory!=bound)goto done;replay=directory;
    if(snprintf(path,sizeof path,"%s/platform-backup",op_path)>=(int)sizeof path)goto done;
    int pn=snprintf(prefix,sizeof prefix,"BROray-platform-backup-executor/1\n%s\n%s\n%s\n%s\n",input->operation,input->nonce,argv[4],native);
    if(pn<0||pn>=(int)sizeof prefix)goto done;
    if(replay){
        base=checked_directory(path);if(base<0||pt_backup_names(base,before)||migration_read(base,"executor.record",&stored,0)||stored.mode!=0600||stored.size<=(size_t)pn||memcmp(stored.bytes,prefix,(size_t)pn)||memchr(stored.bytes,0,stored.size))goto done;
        /* This immutable historical identity grants no authority to a PID.
         * Its exact bytes are pinned by intent + external binding below. */
        long pid=0;unsigned long long start=0;char born[64];int used=0;
        if(sscanf(stored.bytes+pn,"{\"pid\":%ld,\"startTicks\":\"%llu\",\"bootId\":\"%63[^\"]\",\"executable\":%n",&pid,&start,born,&used)!=3||pid<=1||pid>INT_MAX||!start||!used||strlen(born)!=36||!token(born,36)||!strcmp(born,input->old_boot)||stored.bytes[stored.size-1]!='\n')goto done;
        execution=stored.bytes;execution_size=stored.size;stored.bytes=NULL;
    }else{
        FILE *f=open_memstream(&execution,&execution_size);if(!f)goto done;
        fwrite(prefix,1,(size_t)pn,f);identity_json(f,executor);fputc('\n',f);if(fclose(f))goto done;
    }
    digest_bytes(execution,execution_size,execution_sha);
    if(pt_backup_intent(input,argv[4],native,was_running,stopped.sha,execution_sha,before,&intent,&intent_size))goto done;
    digest_bytes(intent,intent_size,intent_sha);
    int bn=snprintf(binding,sizeof binding,"{\"schemaVersion\":1,\"contract\":\"broray-platform-backup-binding/1\",\"operationId\":\"%s\",\"stopNonce\":\"%s\",\"intentSha256\":\"%s\"}\n",input->operation,input->nonce,intent_sha);
    if(bn<0||bn>=(int)sizeof binding||pt_inventory(live,before))goto done;
    if(!replay){
        /* Publication loss is not retried as a fresh backup. Preserve the
         * incomplete pair and fence for explicit protected recovery. */
        if(migration_record(op,"platform-backup.json",binding,(size_t)bn,1)||mkdirat(op,"platform-backup",0700)||fsync(op))goto done;
        base=checked_directory(path);if(base<0||bg_empty(base))goto done;
    }
    if(migration_record(op,"platform-backup.json",binding,(size_t)bn,0)||
       migration_record(base,"executor.record",execution,execution_size,!replay)||
       migration_record(base,"intent.json",intent,intent_size,!replay))goto done;
    for(int i=0;i<MIGRATION_FILES;i++)if(before[i].present){char name[32];snprintf(name,sizeof name,"before-%d",i);if(migration_record(base,name,before[i].bytes,before[i].size,!replay))goto done;}
    /* Only private backup evidence has changed. Verify its exact bytes before
     * the marker, then re-prove the FULL context after publication and before
     * success below. The same installation flock excludes any consumer in
     * between; READY by itself is never authority to install or release it. */
    if(pt_backup_copies(base,before)||pt_inventory(live,before))goto done;
    int tn=snprintf(terminal,sizeof terminal,"BROray-platform-backup-ready/1\n%s\n%s\n",intent_sha,stopped.sha);
    if(tn<0||tn>=(int)sizeof terminal)goto done;digest_bytes(terminal,(size_t)tn,terminal_sha);
    int an=snprintf(anchor_record,sizeof anchor_record,"BROray-platform-backup-ready-anchor/1\n%s\n",terminal_sha);
    if(an<0||an>=(int)sizeof anchor_record||
       bg_record_exact(op,"platform-backup.json",binding,(size_t)bn)||bg_record_exact(base,"intent.json",intent,intent_size)||
       bg_record_exact(base,"executor.record",execution,execution_size)||pt_backup_copies(base,before)||pt_inventory(live,before)||
       migration_record(base,"ready.anchor",anchor_record,(size_t)an,!replay)||
       migration_record(base,"ready.receipt",terminal,(size_t)tn,!replay)||
       pt_backup_names(base,before)||migration_sync_directory(path,base)||
       recovery_context_proof(argv,held,statefd,op_path,shell,controller,input)||
       legacy_retirement_apply(argv,op,op_path,input,native,executor,held,statefd,shell,controller,1)||
       bg_record_exact(op,"platform-backup.json",binding,(size_t)bn)||bg_record_exact(base,"intent.json",intent,intent_size)||
       bg_record_exact(base,"executor.record",execution,execution_size)||pt_backup_copies(base,before)||pt_inventory(live,before)||
       bg_record_exact(base,"ready.anchor",anchor_record,(size_t)an)||bg_record_exact(base,"ready.receipt",terminal,(size_t)tn))goto done;
    printf("{\"ok\":true,\"phase\":\"BACKUP_READY\",\"intentSha256\":\"%s\",\"replayed\":%s,\"activationAllowed\":false}\n",intent_sha,replay?"true":"false");result=0;
done:
    for(int i=0;i<MIGRATION_FILES;i++)free(before[i].bytes);free(stopped.bytes);free(original.bytes);free(stored.bytes);free(execution);free(intent);
    if(base>=0)close(base);if(live>=0)close(live);return result?migration_error("PLATFORM_BACKUP_UNCONFIRMED"):0;
}

/* Load before-images from authenticated evidence, NEVER from a partly changed
 * live installation. The generated refusing guards are the two before-images
 * at this boundary; the original legacy executables remain separately saved. */
static int pt_backup_load(int op,const char *op_path,const struct bg_input *input,const char *migration,const char *native,struct migration_file before[MIGRATION_FILES],char digest[65]){
    int base=-1,result=-1;char path[PATH_MAX],prefix[512],binding[512],terminal[192],anchor[128],terminal_sha[65];
    struct migration_file stopped,original,execution;memset(&stopped,0,sizeof stopped);memset(&original,0,sizeof original);memset(&execution,0,sizeof execution);
    char *intent=NULL;size_t size=0;
    if(snprintf(path,sizeof path,"%s/platform-backup",op_path)>=(int)sizeof path)goto done;
    base=checked_directory(path);
    if(base<0||migration_read(op,"platform-legacy-retirement/stopped.receipt",&stopped,0)||stopped.mode!=0600||
       migration_read(op,"platform-legacy-control/snapshot.json",&original,0)||original.mode!=0600||
       migration_read(base,"executor.record",&execution,0)||execution.mode!=0600)goto done;
    int running=strstr(original.bytes,"\"oldServiceWasRunning\":true,")!=NULL;
    if(!running&&!strstr(original.bytes,"\"oldServiceWasRunning\":false,"))goto done;
    int pn=snprintf(prefix,sizeof prefix,"BROray-platform-backup-executor/1\n%s\n%s\n%s\n%s\n",input->operation,input->nonce,migration,native);
    if(pn<0||pn>=(int)sizeof prefix||execution.size<=(size_t)pn||memcmp(execution.bytes,prefix,(size_t)pn)||memchr(execution.bytes,0,execution.size)||execution.bytes[execution.size-1]!='\n')goto done;
    long pid=0;unsigned long long start=0;char born[64];int used=0;
    if(sscanf(execution.bytes+pn,"{\"pid\":%ld,\"startTicks\":\"%llu\",\"bootId\":\"%63[^\"]\",\"executable\":%n",&pid,&start,born,&used)!=3||pid<=1||pid>INT_MAX||!start||!used||strlen(born)!=36||!token(born,36)||!strcmp(born,input->old_boot))goto done;
    for(int i=0;i<MIGRATION_FILES;i++){
        char name[32];snprintf(name,sizeof name,"before-%d",i);
        if(!input->present[i]){if(bg_exists(base,name)!=0)goto done;strcpy(before[i].sha,"-");continue;}
        if(migration_read(base,name,&before[i],0)||before[i].mode!=0600)goto done;
        if(i==1||i==5){
            char *guard=NULL;size_t n=0;int bad=bg_guard_text(i,migration,input,native,&guard,&n);
            if(!bad)bad=before[i].size!=n||memcmp(before[i].bytes,guard,n);free(guard);if(bad)goto done;before[i].mode=0755;
        }else{if(strcmp(before[i].sha,input->before[i]))goto done;before[i].mode=input->mode[i];}
    }
    if(pt_backup_names(base,before)||pt_backup_intent(input,migration,native,running,stopped.sha,execution.sha,before,&intent,&size))goto done;
    digest_bytes(intent,size,digest);
    int bn=snprintf(binding,sizeof binding,"{\"schemaVersion\":1,\"contract\":\"broray-platform-backup-binding/1\",\"operationId\":\"%s\",\"stopNonce\":\"%s\",\"intentSha256\":\"%s\"}\n",input->operation,input->nonce,digest);
    int tn=snprintf(terminal,sizeof terminal,"BROray-platform-backup-ready/1\n%s\n%s\n",digest,stopped.sha);
    if(bn<0||bn>=(int)sizeof binding||tn<0||tn>=(int)sizeof terminal)goto done;digest_bytes(terminal,(size_t)tn,terminal_sha);
    int an=snprintf(anchor,sizeof anchor,"BROray-platform-backup-ready-anchor/1\n%s\n",terminal_sha);
    if(an<0||an>=(int)sizeof anchor||bg_record_exact(op,"platform-backup.json",binding,(size_t)bn)||
       bg_record_exact(base,"intent.json",intent,size)||bg_record_exact(base,"ready.anchor",anchor,(size_t)an)||
       bg_record_exact(base,"ready.receipt",terminal,(size_t)tn)||pt_backup_copies(base,before))goto done;
    result=0;
done:
    if(base>=0)close(base);free(stopped.bytes);free(original.bytes);free(execution.bytes);free(intent);return result;
}

struct pt_inode {unsigned long long device,inode;};
static int pt_inode_exact(int parent,const char *name,const struct migration_file *file,const struct pt_inode *id){
    if(!file->present)return bg_exists(parent,name)==0&&!id->device&&!id->inode?0:-1;
    struct stat st;
    return bg_exact(parent,name,file->bytes,file->size,file->mode)||fstatat(parent,name,&st,AT_SYMLINK_NOFOLLOW)||
        (unsigned long long)st.st_dev!=id->device||(unsigned long long)st.st_ino!=id->inode||st.st_nlink!=1?-1:0;
}
static int pt_inode_get(int parent,const char *name,const struct migration_file *file,struct pt_inode *id){
    memset(id,0,sizeof *id);if(!file->present)return bg_exists(parent,name)==0?0:-1;
    struct stat st;if(bg_exact(parent,name,file->bytes,file->size,file->mode)||fstatat(parent,name,&st,AT_SYMLINK_NOFOLLOW)||st.st_nlink!=1)return -1;
    id->device=(unsigned long long)st.st_dev;id->inode=(unsigned long long)st.st_ino;return pt_inode_exact(parent,name,file,id);
}
static int pt_install_names(int base){
    DIR *d=directory_stream(base);if(!d)return -1;struct dirent *e;int bad=0;errno=0;
    while((e=readdir(d))){const char *name=e->d_name;if(!strcmp(name,".")||!strcmp(name,"..")||!strcmp(name,"intent.record")||!strcmp(name,"installed.anchor")||!strcmp(name,"installed.receipt"))continue;
        int allowed=0;for(int i=0;i<MIGRATION_FILES;i++){char n[32];const char *suffix[]={"intent","prepared","done"};for(int j=0;j<3;j++){snprintf(n,sizeof n,"entry-%d.%s",i,suffix[j]);if(!strcmp(name,n))allowed=1;}}
        if(!allowed){bad=1;break;}errno=0;
    }
    if(!e&&errno)bad=1;closedir(d);return bad?-1:0;
}
static int pt_entry_record(int base,const char *name,const char *prefix,struct pt_inode *id,int create){
    if(!create){
        struct migration_file f;if(migration_read(base,name,&f,0))return -1;size_t n=strlen(prefix);int used=0;
        int bad=f.mode!=0600||f.size<=n||memcmp(f.bytes,prefix,n)||memchr(f.bytes,0,f.size);
        if(!bad)bad=sscanf(f.bytes+n,"%llu\n%llu\n%n",&id->device,&id->inode,&used)!=2||!used||n+(size_t)used!=f.size;
        free(f.bytes);if(bad)return -1;
    }
    char record[512];int n=snprintf(record,sizeof record,"%s%llu\n%llu\n",prefix,id->device,id->inode);
    return n<0||n>=(int)sizeof record||migration_record(base,name,record,(size_t)n,create)?-1:0;
}
/* Each step preserves its displaced inode. Unknown/missing records or a
 * partial candidate copy remain NEEDS_RECOVERY, never a guessed replacement.
 * A rename lost response is safe to resume from exact recorded identities. */
static int pt_install_entry(int base,int parent,const char *entry,int index,const char *intent,const struct migration_file *before,const struct migration_file *after,int terminal,int mutate){
    char old[128],candidate[128],start[32],prepared[32],done[32],prefix[256];
    snprintf(old,sizeof old,".broray-pt-%s-%d.previous",intent,index);snprintf(candidate,sizeof candidate,".broray-pt-%s-%d.candidate",intent,index);
    snprintf(start,sizeof start,"entry-%d.intent",index);snprintf(prepared,sizeof prepared,"entry-%d.prepared",index);snprintf(done,sizeof done,"entry-%d.done",index);
    snprintf(prefix,sizeof prefix,"BROray-platform-install-entry/1\n%s\n%d\n%s\n%04o\n%s\n",intent,index,before->sha,before->mode,after->sha);
    int started=bg_exists(base,start),staged=bg_exists(base,prepared),completed=bg_exists(base,done);
    int saved=bg_exists(parent,old),current=bg_exists(parent,entry),pending=bg_exists(parent,candidate);
    struct pt_inode previous={0,0},next={0,0};
    if(started<0||staged<0||completed<0||saved<0||current<0||pending<0||(!started&&(staged||completed))||(!staged&&completed)||
       (terminal&&(!started||!staged||!completed)))return -1;
    if(!started){
        if(saved||pending||pt_inode_get(parent,entry,before,&previous))return -1;
        if(!mutate)return 0;
        if(pt_entry_record(base,start,prefix,&previous,1))return -1;
    }else if(pt_entry_record(base,start,prefix,&previous,0))return -1;
    if(saved){if(!before->present||pt_inode_exact(parent,old,before,&previous))return -1;}
    else if(before->present&&pt_inode_exact(parent,entry,before,&previous))return -1;
    else if(!before->present&&(previous.device||previous.inode))return -1;
    if(!staged){
        if(saved||completed||current!=before->present||pt_inode_exact(parent,entry,before,&previous))return -1;
        /* An unbound candidate may have been written by another process or
         * interrupted mid-copy. Preserve it and refuse to adopt it. */
        if(pending)return -1;
        if(!mutate)return 0;
        if(bg_candidate(parent,candidate,after->bytes,after->size,after->mode)||pt_inode_get(parent,candidate,after,&next)||pt_entry_record(base,prepared,prefix,&next,1))return -1;
        pending=1;
    }else if(pt_entry_record(base,prepared,prefix,&next,0)||!next.device||!next.inode)return -1;
    if(pending){
        if(completed||pt_inode_exact(parent,candidate,after,&next))return -1;
        if(saved){if(current)return -1;}
        else if(pt_inode_exact(parent,entry,before,&previous))return -1;
        if(!mutate)return 0;
        if(before->present&&!saved){if(bg_move(parent,entry,old)||pt_inode_exact(parent,old,before,&previous))return -1;}
        if(bg_exists(parent,entry)!=0||pt_inode_exact(parent,candidate,after,&next)||bg_move(parent,candidate,entry))return -1;
    }else if(current!=1||saved!=before->present||pt_inode_exact(parent,entry,after,&next))return -1;
    if(pt_inode_exact(parent,entry,after,&next)||(before->present&&pt_inode_exact(parent,old,before,&previous))||
       bg_exists(parent,candidate)!=0||bg_sync_file(parent,entry,after->mode)||fsync(parent))return -1;
    if(!mutate&&!completed)return 0;
    struct pt_inode acknowledged=next;
    if(pt_entry_record(base,done,prefix,&acknowledged,!completed&&!terminal)||
       acknowledged.device!=next.device||acknowledged.inode!=next.inode)return -1;
    return 0;
}

static int platform_install_apply(char **argv,int op,const char *op_path,const struct bg_input *input,const char *native,const struct identity *executor,int held,int statefd,const char *shell,const char *controller,int verify_only){
    int live=-1,base=-1,mig=-1,result=75,parents[MIGRATION_FILES],replay=0;for(int i=0;i<MIGRATION_FILES;i++)parents[i]=-1;
    struct migration_file before[MIGRATION_FILES],after[MIGRATION_FILES],recheck[MIGRATION_FILES];memset(before,0,sizeof before);memset(after,0,sizeof after);memset(recheck,0,sizeof recheck);
    char entries[MIGRATION_FILES][NAME_MAX+1],path[PATH_MAX],backup[65],again[65],intent[768],intent_sha[65],binding[160],terminal[192],anchor[128],terminal_sha[65];
    if(legacy_retirement_apply(argv,op,op_path,input,native,executor,held,statefd,shell,controller,1)||pt_backup_load(op,op_path,input,argv[4],native,before,backup))goto done;
    live=migration_directory(argv[2]);
    if(snprintf(path,sizeof path,"%s/platform-migration",op_path)>=(int)sizeof path)goto done;mig=checked_directory(path);if(live<0||mig<0)goto done;
    for(int i=0;i<MIGRATION_FILES;i++){
        char name[32];snprintf(name,sizeof name,"file-%d",i);
        if(migration_read(mig,name,&after[i],0)||after[i].mode!=0600||strcmp(after[i].sha,input->after[i]))goto done;after[i].mode=0755;
        parents[i]=bg_parent(live,migration_paths[i],entries[i]);if(parents[i]<0)goto done;
    }
    int n=snprintf(intent,sizeof intent,"BROray-platform-install/1\nINSTALLING\n%s\n%s\n%s\n%s\n%s\n%s\n",input->operation,input->nonce,argv[4],input->manifest,native,backup);
    if(n<0||n>=(int)sizeof intent)goto done;digest_bytes(intent,(size_t)n,intent_sha);
    int bn=snprintf(binding,sizeof binding,"BROray-platform-install-binding/1\n%s\n",intent_sha);
    if(bn<0||bn>=(int)sizeof binding)goto done;
    int exists=bg_exists(op,"platform-install"),bound=bg_exists(op,"platform-install.record");if(exists<0||bound<0||exists!=bound)goto done;
    if(snprintf(path,sizeof path,"%s/platform-install",op_path)>=(int)sizeof path||(verify_only&&!exists))goto done;
    if(!exists){
        if(pt_inventory(live,before)||migration_record(op,"platform-install.record",binding,(size_t)bn,1)||mkdirat(op,"platform-install",0700)||fsync(op))goto done;
    }
    base=checked_directory(path);if(base<0||(!exists&&bg_empty(base))||pt_install_names(base)||
       migration_record(op,"platform-install.record",binding,(size_t)bn,0)||migration_record(base,"intent.record",intent,(size_t)n,!exists))goto done;
    int complete=bg_exists(base,"installed.receipt"),anchored=bg_exists(base,"installed.anchor");if(complete<0||anchored<0||complete!=anchored||(verify_only&&!complete))goto done;replay=complete;
    /* Check every file state before advancing even the first one. */
    for(int i=0;i<MIGRATION_FILES;i++)if(pt_install_entry(base,parents[i],entries[i],i,intent_sha,&before[i],&after[i],complete,0))goto done;
    for(int i=0;i<MIGRATION_FILES;i++){
        if(bg_record_exact(op,"platform-install.record",binding,(size_t)bn)||bg_record_exact(base,"intent.record",intent,(size_t)n)||
           pt_install_names(base)||pt_install_entry(base,parents[i],entries[i],i,intent_sha,&before[i],&after[i],complete,!verify_only))goto done;
    }
    if(pt_inventory(live,after)||recovery_context_proof(argv,held,statefd,op_path,shell,controller,input)||
       legacy_retirement_apply(argv,op,op_path,input,native,executor,held,statefd,shell,controller,1)||
       pt_backup_load(op,op_path,input,argv[4],native,recheck,again)||strcmp(backup,again))goto done;
    int tn=snprintf(terminal,sizeof terminal,"BROray-platform-installed/1\n%s\n%s\n",intent_sha,input->manifest);if(tn<0||tn>=(int)sizeof terminal)goto done;digest_bytes(terminal,(size_t)tn,terminal_sha);
    int an=snprintf(anchor,sizeof anchor,"BROray-platform-installed-anchor/1\n%s\n",terminal_sha);
    if(an<0||an>=(int)sizeof anchor||migration_record(base,"installed.anchor",anchor,(size_t)an,!complete)||migration_record(base,"installed.receipt",terminal,(size_t)tn,!complete)||
       migration_sync_directory(path,base)||pt_install_names(base)||pt_inventory(live,after)||
       bg_record_exact(op,"platform-install.record",binding,(size_t)bn)||bg_record_exact(base,"intent.record",intent,(size_t)n)||
       bg_record_exact(base,"installed.anchor",anchor,(size_t)an)||bg_record_exact(base,"installed.receipt",terminal,(size_t)tn))goto done;
    for(int i=0;i<MIGRATION_FILES;i++)if(pt_install_entry(base,parents[i],entries[i],i,intent_sha,&before[i],&after[i],1,0))goto done;
    if(!verify_only)printf("{\"ok\":true,\"phase\":\"INSTALLED\",\"replayed\":%s,\"activationAllowed\":false}\n",replay?"true":"false");result=0;
done:
    for(int i=0;i<MIGRATION_FILES;i++){free(before[i].bytes);free(after[i].bytes);free(recheck[i].bytes);if(parents[i]>=0)close(parents[i]);}
    if(base>=0)close(base);if(live>=0)close(live);if(mig>=0)close(mig);return result?migration_error("PLATFORM_INSTALL_NEEDS_RECOVERY"):0;
}

static int pt_rollback_names(int base){
    DIR *d=directory_stream(base);if(!d)return -1;struct dirent *e;int bad=0;errno=0;
    while((e=readdir(d))){const char *name=e->d_name;if(!strcmp(name,".")||!strcmp(name,"..")||!strcmp(name,"intent.record")||!strcmp(name,"restored.anchor")||!strcmp(name,"restored.receipt"))continue;
        int allowed=0;for(int i=0;i<MIGRATION_FILES;i++){char n[32];snprintf(n,sizeof n,"entry-%d.intent",i);if(!strcmp(name,n))allowed=1;snprintf(n,sizeof n,"entry-%d.done",i);if(!strcmp(name,n))allowed=1;}
        if(!allowed){bad=1;break;}errno=0;
    }
    if(!e&&errno)bad=1;closedir(d);return bad?-1:0;
}
static int pt_rollback_entry(int base,int install,int parent,const char *entry,int index,const char *intent,const char *rollback,const struct migration_file *before,const struct migration_file *after,int installed,int terminal,int mutate){
    char old[128],candidate[128],rejected[128],start[32],prepared[32],done[32],prefix[256],proof[512];
    snprintf(old,sizeof old,".broray-pt-%s-%d.previous",intent,index);snprintf(candidate,sizeof candidate,".broray-pt-%s-%d.candidate",intent,index);
    snprintf(rejected,sizeof rejected,".broray-pt-%s-%d.rejected",intent,index);
    snprintf(start,sizeof start,"entry-%d.intent",index);snprintf(prepared,sizeof prepared,"entry-%d.prepared",index);snprintf(done,sizeof done,"entry-%d.done",index);
    snprintf(prefix,sizeof prefix,"BROray-platform-install-entry/1\n%s\n%d\n%s\n%04o\n%s\n",intent,index,before->sha,before->mode,after->sha);
    int started=bg_exists(install,start),staged=bg_exists(install,prepared),completed=bg_exists(install,done);
    int rb_started=bg_exists(base,start),rb_done=bg_exists(base,done);
    int saved=bg_exists(parent,old),pending=bg_exists(parent,candidate),discarded=bg_exists(parent,rejected),current=bg_exists(parent,entry),kind=0;
    struct pt_inode previous={0,0},next={0,0},acknowledged;
    if(started<0||staged<0||completed<0||rb_started<0||rb_done<0||saved<0||pending<0||discarded<0||current<0||
       (!started&&(staged||completed))||(!staged&&completed)||(!rb_started&&(rb_done||discarded))||
       (installed&&(!started||!staged||!completed))||(terminal&&(!rb_started||!rb_done)))return -1;
    if(started){if(pt_entry_record(install,start,prefix,&previous,0))return -1;}
    else if(pt_inode_get(parent,entry,before,&previous))return -1;
    if(staged){if(pt_entry_record(install,prepared,prefix,&next,0)||!next.device||!next.inode)return -1;}
    if(completed){acknowledged=next;if(pt_entry_record(install,done,prefix,&acknowledged,0)||acknowledged.device!=next.device||acknowledged.inode!=next.inode)return -1;}
    if(current){
        if(before->present&&!pt_inode_exact(parent,entry,before,&previous))kind=1;
        else if(staged&&!pt_inode_exact(parent,entry,after,&next))kind=2;
        else return -1;
    }
    if(saved&&(!started||!before->present||pt_inode_exact(parent,old,before,&previous)))return -1;
    if(pending&&(!staged||pt_inode_exact(parent,candidate,after,&next)))return -1;
    if(discarded&&(!staged||pt_inode_exact(parent,rejected,after,&next)))return -1;
    if((kind==1)+saved!=before->present||(kind==2)+pending+discarded!=staged||
       (!before->present&&(previous.device||previous.inode))||(!started&&(saved||pending||discarded))||
       (rb_done&&(saved||kind!=before->present)))return -1;
    int n=snprintf(proof,sizeof proof,"BROray-platform-rollback-entry/1\n%s\n%s\n%d\n%d\n%d\n%d\n%llu\n%llu\n%llu\n%llu\n",rollback,intent,index,started,staged,completed,previous.device,previous.inode,next.device,next.inode);
    if(n<0||n>=(int)sizeof proof)return -1;
    if(rb_started&&bg_record_exact(base,start,proof,(size_t)n))return -1;
    if(rb_done&&bg_record_exact(base,done,proof,(size_t)n))return -1;
    if(!mutate)return 0;
    if(migration_record(base,start,proof,(size_t)n,!rb_started&&!terminal))return -1;
    if(kind==2){
        if(discarded||pt_inode_exact(parent,entry,after,&next)||bg_move(parent,entry,rejected)||pt_inode_exact(parent,rejected,after,&next))return -1;
        discarded=1;kind=0;
    }
    if(saved){
        if(kind||bg_exists(parent,entry)!=0||pt_inode_exact(parent,old,before,&previous)||bg_move(parent,old,entry))return -1;
    }
    if(pt_inode_exact(parent,entry,before,&previous)||bg_exists(parent,old)!=0||
       (pending&&pt_inode_exact(parent,candidate,after,&next))||(discarded&&pt_inode_exact(parent,rejected,after,&next))||
       (before->present&&bg_sync_file(parent,entry,before->mode))||fsync(parent)||
       bg_record_exact(base,start,proof,(size_t)n)||migration_record(base,done,proof,(size_t)n,!rb_done&&!terminal))return -1;
    return 0;
}

static int platform_rollback_apply(char **argv,int op,const char *op_path,const struct bg_input *input,const char *native,const struct identity *executor,int held,int statefd,const char *shell,const char *controller){
    int live=-1,base=-1,install=-1,mig=-1,generation_lock=-1,result=75,parents[MIGRATION_FILES];for(int i=0;i<MIGRATION_FILES;i++)parents[i]=-1;
    struct migration_file before[MIGRATION_FILES],after[MIGRATION_FILES],recheck[MIGRATION_FILES];memset(before,0,sizeof before);memset(after,0,sizeof after);memset(recheck,0,sizeof recheck);
    char entries[MIGRATION_FILES][NAME_MAX+1],path[PATH_MAX],backup[65],again[65],intent[768],intent_sha[65],binding[160],terminal[192],anchor[128],terminal_sha[65];
    char rollback[320],rollback_sha[65],rb_binding[160],rb_terminal[256],rb_anchor[128];
    if(platform_rollback_generation_guard(op,op_path,argv[2],input,argv[4],native,&generation_lock,1)||
       legacy_retirement_apply(argv,op,op_path,input,native,executor,held,statefd,shell,controller,1)||pt_backup_load(op,op_path,input,argv[4],native,before,backup))goto done;
    live=migration_directory(argv[2]);if(snprintf(path,sizeof path,"%s/platform-migration",op_path)>=(int)sizeof path)goto done;
    mig=checked_directory(path);if(live<0||mig<0)goto done;
    for(int i=0;i<MIGRATION_FILES;i++){
        char name[32];snprintf(name,sizeof name,"file-%d",i);
        if(migration_read(mig,name,&after[i],0)||after[i].mode!=0600||strcmp(after[i].sha,input->after[i]))goto done;after[i].mode=0755;
        parents[i]=bg_parent(live,migration_paths[i],entries[i]);if(parents[i]<0)goto done;
    }
    int n=snprintf(intent,sizeof intent,"BROray-platform-install/1\nINSTALLING\n%s\n%s\n%s\n%s\n%s\n%s\n",input->operation,input->nonce,argv[4],input->manifest,native,backup);
    if(n<0||n>=(int)sizeof intent)goto done;digest_bytes(intent,(size_t)n,intent_sha);
    int bn=snprintf(binding,sizeof binding,"BROray-platform-install-binding/1\n%s\n",intent_sha);
    if(bn<0||bn>=(int)sizeof binding||snprintf(path,sizeof path,"%s/platform-install",op_path)>=(int)sizeof path)goto done;
    install=checked_directory(path);if(install<0||pt_install_names(install)||bg_record_exact(op,"platform-install.record",binding,(size_t)bn)||bg_record_exact(install,"intent.record",intent,(size_t)n))goto done;
    int installed=bg_exists(install,"installed.receipt"),install_anchor=bg_exists(install,"installed.anchor");if(installed<0||install_anchor<0||installed!=install_anchor)goto done;
    int tn=snprintf(terminal,sizeof terminal,"BROray-platform-installed/1\n%s\n%s\n",intent_sha,input->manifest);if(tn<0||tn>=(int)sizeof terminal)goto done;digest_bytes(terminal,(size_t)tn,terminal_sha);
    int an=snprintf(anchor,sizeof anchor,"BROray-platform-installed-anchor/1\n%s\n",terminal_sha);
    if(an<0||an>=(int)sizeof anchor||(installed&&(bg_record_exact(install,"installed.receipt",terminal,(size_t)tn)||bg_record_exact(install,"installed.anchor",anchor,(size_t)an))))goto done;
    int rn=snprintf(rollback,sizeof rollback,"BROray-platform-rollback/1\nROLLING_BACK\n%s\n%s\n%s\n",intent_sha,backup,native);
    if(rn<0||rn>=(int)sizeof rollback)goto done;digest_bytes(rollback,(size_t)rn,rollback_sha);
    int rbn=snprintf(rb_binding,sizeof rb_binding,"BROray-platform-rollback-binding/1\n%s\n",rollback_sha);
    if(rbn<0||rbn>=(int)sizeof rb_binding)goto done;
    int exists=bg_exists(op,"platform-rollback"),bound=bg_exists(op,"platform-rollback.record");if(exists<0||bound<0||exists!=bound)goto done;
    if(snprintf(path,sizeof path,"%s/platform-rollback",op_path)>=(int)sizeof path)goto done;
    if(!exists){
        /* Prove the complete install state before publishing rollback intent.
         * This verifier cannot reconstruct a missing done record. */
        for(int i=0;i<MIGRATION_FILES;i++){
            char rejected[128];snprintf(rejected,sizeof rejected,".broray-pt-%s-%d.rejected",intent_sha,i);
            if(bg_exists(parents[i],rejected)!=0||pt_install_entry(install,parents[i],entries[i],i,intent_sha,&before[i],&after[i],installed,0))goto done;
        }
        if(migration_record(op,"platform-rollback.record",rb_binding,(size_t)rbn,1)||mkdirat(op,"platform-rollback",0700)||fsync(op))goto done;
    }
    base=checked_directory(path);if(base<0||(!exists&&bg_empty(base))||pt_rollback_names(base)||
       migration_record(op,"platform-rollback.record",rb_binding,(size_t)rbn,0)||migration_record(base,"intent.record",rollback,(size_t)rn,!exists))goto done;
    int complete=bg_exists(base,"restored.receipt"),anchored=bg_exists(base,"restored.anchor");if(complete<0||anchored<0||complete!=anchored)goto done;
    for(int i=0;i<MIGRATION_FILES;i++)if(pt_rollback_entry(base,install,parents[i],entries[i],i,intent_sha,rollback_sha,&before[i],&after[i],installed,complete,0))goto done;
    for(int i=0;i<MIGRATION_FILES;i++){
        if(platform_rollback_generation_guard(op,op_path,argv[2],input,argv[4],native,&generation_lock,0)||
           pt_rollback_names(base)||pt_install_names(install)||bg_record_exact(op,"platform-rollback.record",rb_binding,(size_t)rbn)||
           bg_record_exact(base,"intent.record",rollback,(size_t)rn)||bg_record_exact(op,"platform-install.record",binding,(size_t)bn)||bg_record_exact(install,"intent.record",intent,(size_t)n)||
           pt_rollback_entry(base,install,parents[i],entries[i],i,intent_sha,rollback_sha,&before[i],&after[i],installed,complete,1))goto done;
    }
    char *strict[]={argv[0],"recovery-backup",argv[2],argv[3],argv[4],argv[5],NULL};
    if(platform_rollback_generation_guard(op,op_path,argv[2],input,argv[4],native,&generation_lock,0)||
       pt_inventory(live,before)||recovery_context_proof(strict,held,statefd,op_path,shell,controller,input)||
       legacy_retirement_apply(argv,op,op_path,input,native,executor,held,statefd,shell,controller,1)||
       pt_backup_load(op,op_path,input,argv[4],native,recheck,again)||strcmp(backup,again))goto done;
    int rtn=snprintf(rb_terminal,sizeof rb_terminal,"BROray-platform-restored-guarded/1\n%s\n%s\nservice-state-not-restored\n",rollback_sha,backup);
    if(rtn<0||rtn>=(int)sizeof rb_terminal)goto done;digest_bytes(rb_terminal,(size_t)rtn,terminal_sha);
    int ran=snprintf(rb_anchor,sizeof rb_anchor,"BROray-platform-rollback-anchor/1\n%s\n",terminal_sha);
    if(ran<0||ran>=(int)sizeof rb_anchor||migration_record(base,"restored.anchor",rb_anchor,(size_t)ran,!complete)||migration_record(base,"restored.receipt",rb_terminal,(size_t)rtn,!complete)||
       migration_sync_directory(path,base)||pt_rollback_names(base)||pt_inventory(live,before)||
       bg_record_exact(op,"platform-rollback.record",rb_binding,(size_t)rbn)||bg_record_exact(base,"intent.record",rollback,(size_t)rn)||
       bg_record_exact(base,"restored.anchor",rb_anchor,(size_t)ran)||bg_record_exact(base,"restored.receipt",rb_terminal,(size_t)rtn))goto done;
    for(int i=0;i<MIGRATION_FILES;i++)if(pt_rollback_entry(base,install,parents[i],entries[i],i,intent_sha,rollback_sha,&before[i],&after[i],installed,1,0))goto done;
    if(platform_rollback_generation_guard(op,op_path,argv[2],input,argv[4],native,&generation_lock,0))goto done;
    /* Exact guarded bytes are restored. Starting the former unsupervised
     * daemon is forbidden; retain the original fence and report that limit. */
    printf("{\"ok\":false,\"phase\":\"NEEDS_RECOVERY\",\"platformRestored\":true,\"serviceStateRestored\":false,\"replayed\":%s,\"activationAllowed\":false}\n",complete?"true":"false");result=0;
done:
    for(int i=0;i<MIGRATION_FILES;i++){free(before[i].bytes);free(after[i].bytes);free(recheck[i].bytes);if(parents[i]>=0)close(parents[i]);}
    if(generation_lock>=0)close(generation_lock);if(base>=0)close(base);if(install>=0)close(install);if(live>=0)close(live);if(mig>=0)close(mig);
    return migration_error(result?"PLATFORM_ROLLBACK_UNCONFIRMED":"PLATFORM_ROLLBACK_SERVICE_REQUIRES_SUPERVISION");
}
