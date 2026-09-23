/* Observation-only legacy control inventory. These records permit NO signal,
 * cleanup or STOPPED; boot recovery must independently prove the old boot ended.
 * Source is the protected coordinator; live metadata is never changed here. */
static const char *lc_names[2]={"daemon.pid","daemon.ready"};
struct lc_inventory {struct migration_file file[2];int lock_present;unsigned lock_mode;};
/* Set only by a verified retirement binding while checking historic metadata.
 * A present live AND retired object is ambiguous, never a preferred copy. */
static int lc_retired=-1;
static int lc_new_generation;
static int platform_started_current(int op,const char *op_path,const char *root,const struct bg_input *input,const char *migration,const char *native);
static int platform_stop_current_proof(int op,const char *op_path,const char *root,const struct bg_input *input,const char *migration,const char *native);
static int platform_stopped_current_proof(int op,const char *op_path,const char *root,const struct bg_input *input,const char *migration,const char *native);
static int platform_rollback_generation_guard(int op,const char *op_path,const char *root,const struct bg_input *input,const char *migration,const char *native,int *held,int acquire);
static int platform_context_op(int op);
static int platform_attempt_current(int op);
static int platform_attempt_load(int op,const char *op_path,const char *root,const struct bg_input *input,const char *migration,const char *native);
static int platform_attempt_prestart(int op,const char *op_path,const char *root,const struct bg_input *input,const char *migration,const char *native);
static int platform_attempt_control_view(int op,const char *op_path,const char *root,const struct bg_input *input,const char *migration,const char *native);
static int lc_generation_proof(char **argv,int op,const char *op_path,const struct bg_input *input,const char *native){
    if(platform_attempt_load(op,op_path,argv[2],input,argv[4],native))return -1;
    op=platform_attempt_current(op);if(op<0)return -1;
    if(!strcmp(argv[1],"recovery-retry"))return platform_attempt_control_view(op,op_path,argv[2],input,argv[4],native);
    if(!platform_attempt_prestart(op,op_path,argv[2],input,argv[4],native))return 0;
    if(!strcmp(argv[1],"recovery-rollback")||!strcmp(argv[1],"recovery-preserve"))return platform_stopped_current_proof(op,op_path,argv[2],input,argv[4],native);
    if(!strcmp(argv[1],"recovery-stop-current"))return platform_stop_current_proof(op,op_path,argv[2],input,argv[4],native);
    if(strcmp(argv[1],"recovery-start")&&strcmp(argv[1],"recovery-commit")&&strcmp(argv[1],"recovery-commit-check"))return -1;
    return platform_started_current(op,op_path,argv[2],input,argv[4],native);
}
static int lc_retirement_open(int op,const char *op_path,const struct bg_input *input,const char *migration_sha,const char *native);
static int recovery_context_proof(char **argv,int held,int statefd,const char *op,const char *shell,const char *controller,const struct bg_input *input);
static void lc_free(struct lc_inventory *r){for(int i=0;i<2;i++)free(r->file[i].bytes);}
static int lc_owner_valid(const char *expected){
    if(!strcmp(expected,"null"))return 0;
    if(strlen(expected)>PATH_MAX+512U)return -1;
    long pid=0;if(sscanf(expected,"{\"pid\":%ld,",&pid)!=1||pid<=1||pid>INT_MAX)return -1;
    struct identity id;if(capture((pid_t)pid,&id))return -1;
    char *actual=NULL;size_t size=0;FILE *f=open_memstream(&actual,&size);if(!f)return -1;
    identity_json(f,&id);int bad=fclose(f);if(!bad)bad=strlen(expected)!=size||memcmp(actual,expected,size);
    free(actual);return bad?-1:1;
}
static int lc_pid_value(const struct migration_file *file,const char *owner){
    long pid=0;if(sscanf(owner,"{\"pid\":%ld,",&pid)!=1)return -1;
    char expected[32];int n=snprintf(expected,sizeof expected,"%ld",pid);
    if(n<1||file->size<(size_t)n||file->size>32||memcmp(file->bytes,expected,(size_t)n))return -1;
    for(size_t i=(size_t)n;i<file->size;i++)if(file->bytes[i]!='\n')return -1;return 0;
}
static int lc_objects_allowed(int fd){
    DIR *d=directory_stream(fd);if(!d)return -1;struct dirent *e;unsigned seen=0;int bad=0;errno=0;
    while((e=readdir(d))){const char *n=e->d_name;if(!strcmp(n,".")||!strcmp(n,".."))continue;
        unsigned bit=!strcmp(n,"daemon.pid")?1U:!strcmp(n,"daemon.ready")?2U:!strcmp(n,"daemon.lock")?4U:0U;
        if(!bit||(seen&bit)){bad=1;break;}seen|=bit;errno=0;
    }
    if(!e&&errno)bad=1;closedir(d);return bad?-1:0;
}
static int lc_read(int root,const char *owner,struct lc_inventory *out){
    if(lc_new_generation){
        /* This view is enabled only after authenticated proof of the exact
         * transaction's new generation and its distinct live projections.
         * Historic legacy evidence still comes solely from retired objects. */
        if(lc_retired<0||lc_objects_allowed(lc_retired))return -1;int saved=lc_retired;lc_retired=-1;lc_new_generation=0;
        int result=lc_read(saved,owner,out);lc_new_generation=1;lc_retired=saved;return result;
    }
    memset(out,0,sizeof *out);
    if(lc_retired>=0&&lc_objects_allowed(lc_retired))return -1;
    for(int i=0;i<2;i++){
        if(migration_read(root,lc_names[i],&out->file[i],1)||out->file[i].size>32)return -1;
        if(lc_retired>=0){struct migration_file saved;
            if(migration_read(lc_retired,lc_names[i],&saved,1)||saved.size>32)return -1;
            if(saved.present&&out->file[i].present){free(saved.bytes);return -1;}
            if(saved.present)out->file[i]=saved;else free(saved.bytes);
        }
    }
    struct stat st;int fd=-1;
    int lock_root=root;
    if(lc_retired>=0){int original=bg_exists(root,"daemon.lock"),saved=bg_exists(lc_retired,"daemon.lock");
        if(original<0||saved<0||(original&&saved))return -1;if(saved)lock_root=lc_retired;
    }
    if(!fstatat(lock_root,"daemon.lock",&st,AT_SYMLINK_NOFOLLOW)){
        if(!S_ISDIR(st.st_mode)||st.st_uid!=geteuid()||(st.st_mode&07022))return -1;
        fd=openat(lock_root,"daemon.lock",O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);struct stat held;
        if(fd<0)return -1;
        int bad=fstat(fd,&held)||st.st_dev!=held.st_dev||st.st_ino!=held.st_ino||bg_empty(fd);close(fd);if(bad)return -1;
        out->lock_present=1;out->lock_mode=st.st_mode&0777;
    }else if(errno!=ENOENT)return -1;
    if(!strcmp(owner,"null"))return out->file[0].present||out->file[1].present||out->lock_present?-1:0;
    if(!out->file[0].present||!out->lock_present||lc_pid_value(&out->file[0],owner))return -1;
    return out->file[1].present?lc_pid_value(&out->file[1],owner):0;
}
static int lc_same(const struct lc_inventory *a,const struct lc_inventory *b){
    if(a->lock_present!=b->lock_present||a->lock_mode!=b->lock_mode)return -1;
    for(int i=0;i<2;i++)if(a->file[i].present!=b->file[i].present||a->file[i].mode!=b->file[i].mode||strcmp(a->file[i].sha,b->file[i].sha))return -1;
    return 0;
}
static int lc_live_exact(int root,const char *owner,const struct lc_inventory *expected,int current_owner){
    struct lc_inventory now;int rc=lc_read(root,owner,&now);
    if(!rc)rc=lc_same(expected,&now);lc_free(&now);
    return rc||(current_owner&&lc_owner_valid(owner)<0)?-1:0;
}
static int lc_directory_exact(int base,const struct lc_inventory *r){
    DIR *d=directory_stream(base);if(!d)return -1;struct dirent *e;unsigned seen=0;int bad=0;errno=0;
    unsigned wanted=3U|(r->file[0].present?4U:0U)|(r->file[1].present?8U:0U);
    while((e=readdir(d))){const char *n=e->d_name;if(!strcmp(n,".")||!strcmp(n,".."))continue;
        unsigned bit=!strcmp(n,"snapshot.json")?1U:!strcmp(n,"staged.receipt")?2U:!strcmp(n,"file-0")?4U:!strcmp(n,"file-1")?8U:0U;
        if(!bit||!(wanted&bit)||(seen&bit)){bad=1;break;}seen|=bit;errno=0;
    }
    if(!e&&errno)bad=1;closedir(d);return bad||seen!=wanted?-1:0;
}
static int lc_service_exact(int op,const char *sha){
    struct migration_file file;if(migration_read(op,"platform-service.json",&file,0))return -1;
    int bad=file.mode!=0600||strcmp(file.sha,sha);free(file.bytes);return bad?-1:0;
}
static int legacy_control_impl(int argc,char **argv,int emit){
    /* legacy-control-stage|legacy-control-verify OP LIVE MIGRATION MIGRATION_SHA SERVICE_SHA OWNER_JSON */
    if(argc!=8||!migration_path(argv[2])||!migration_path(argv[3])||!migration_path(argv[4])||!hex64(argv[5])||!hex64(argv[6]))return 64;
    int verify=!strcmp(argv[1],"legacy-control-verify"),same_boot=0;
    int op=-1,mig=-1,live=-1,base=-1,result=75;struct bg_input input;memset(&input,0,sizeof input);
    struct lc_inventory before;memset(&before,0,sizeof before);char *snapshot=NULL;size_t size=0;
    char path[PATH_MAX],directory[PATH_MAX],native[65],guard[768],guard_sha[65],binding[1024],snapshot_sha[65],receipt[160];umask(077);
    if(snprintf(path,sizeof path,"%s/platform-migration",argv[2])>=(int)sizeof path||strcmp(path,argv[4])||
       snprintf(directory,sizeof directory,"%s/platform-legacy-control",argv[2])>=(int)sizeof directory)goto done;
    op=checked_directory(argv[2]);mig=checked_directory(argv[4]);
    if(op<0||mig<0||flock(op,LOCK_EX|LOCK_NB)||flock(mig,LOCK_SH|LOCK_NB)||migration_boot(boot)||peer_executable_hash(getpid(),native)||
       bg_source(mig,argv[4],argv[3],argv[5],&input)||lc_service_exact(op,argv[6]))goto done;
    same_boot=!strcmp(boot,input.old_boot);if(!verify&&!same_boot)goto done;
    const char *name=strrchr(argv[2],'/');if(!name||strcmp(name+1,input.operation))goto done;
    int gn=bg_binding_text(guard,&input,argv[5],native);
    if(gn<0||bg_bound_exact(op,guard,(size_t)gn))goto done;digest_bytes(guard,(size_t)gn,guard_sha);
    if(verify){lc_retired=lc_retirement_open(op,argv[2],&input,argv[5],native);if(lc_retired==-2)goto done;}
    if(snprintf(path,sizeof path,"%s%sopt/var/lib/broray-updater",argv[3],strcmp(argv[3],"/")?"/":"")>=(int)sizeof path)goto done;
    /* A previous-boot identity is evidence only, never a current signal target.
     * Verify still requires the exact immutable snapshot/binding from staging. */
    live=checked_directory(path);int owner=same_boot?lc_owner_valid(argv[7]):!!strcmp(argv[7],"null");
    if(live<0||owner<0)goto done;
    if(verify&&lc_retired>=0&&bg_exists(live,"generations")!=0){
        if(same_boot||platform_attempt_control_view(op,argv[2],argv[3],&input,argv[5],native))goto done;
        lc_new_generation=1;
    }
    if(lc_read(live,argv[7],&before))goto done;
    FILE *f=open_memstream(&snapshot,&size);if(!f)goto done;
    fprintf(f,"{\"schemaVersion\":1,\"contract\":\"broray-legacy-updater-control/1\",\"operationId\":\"%s\",\"stopNonce\":\"%s\",\"oldBootId\":\"%s\",\"nativeSha256\":\"%s\",\"migrationIntentSha256\":\"%s\",\"bootGuardBindingSha256\":\"%s\",\"serviceReceiptSha256\":\"%s\",\"oldServiceWasRunning\":%s,\"oldOwner\":%s,\"entries\":[",input.operation,input.nonce,input.old_boot,native,argv[5],guard_sha,argv[6],owner?"true":"false",argv[7]);
    for(int i=0;i<2;i++){
        if(i)fputc(',',f);fprintf(f,"{\"name\":\"%s\",\"kind\":\"file\",\"present\":%s,\"mode\":%u,\"sha256\":",lc_names[i],before.file[i].present?"true":"false",before.file[i].mode);
        if(before.file[i].present)json_string(f,before.file[i].sha);else fputs("null",f);fputc('}',f);
    }
    fprintf(f,",{\"name\":\"daemon.lock\",\"kind\":\"directory\",\"present\":%s,\"mode\":%u,\"empty\":%s}],\"signalsAuthorized\":false,\"serviceStopped\":false,\"activationAllowed\":false}\n",before.lock_present?"true":"false",before.lock_mode,before.lock_present?"true":"false");
    if(fclose(f))goto done;digest_bytes(snapshot,size,snapshot_sha);
    int bn=snprintf(binding,sizeof binding,"{\"schemaVersion\":1,\"contract\":\"broray-legacy-control-binding/1\",\"operationId\":\"%s\",\"stopNonce\":\"%s\",\"snapshotSha256\":\"%s\",\"bootGuardBindingSha256\":\"%s\"}\n",input.operation,input.nonce,snapshot_sha,guard_sha);
    int rn=snprintf(receipt,sizeof receipt,"BROray-legacy-control-staged/1\n%s\n",snapshot_sha);
    if(bn<0||bn>=(int)sizeof binding||rn<0||rn>=(int)sizeof receipt)goto done;
    int prior=bg_exists(op,"platform-legacy-control.json");if(prior<0||(verify&&!prior))goto done;
    if(!prior&&bg_exists(op,"platform-legacy-control")!=0)goto done;
    if(bg_bound_exact(op,guard,(size_t)gn)||lc_service_exact(op,argv[6])||lc_live_exact(live,argv[7],&before,same_boot)||
       migration_record(op,"platform-legacy-control.json",binding,(size_t)bn,!prior))goto done;
    if(!prior&&(mkdirat(op,"platform-legacy-control",0700)||fsync(op)))goto done;
    base=checked_directory(directory);if(base<0||flock(base,LOCK_EX|LOCK_NB))goto done;
    if(prior?lc_directory_exact(base,&before):bg_empty(base))goto done;
    if(migration_record(base,"snapshot.json",snapshot,size,!prior))goto done;
    for(int i=0;i<2;i++)if(before.file[i].present){char file[16];snprintf(file,sizeof file,"file-%d",i);if(migration_record(base,file,before.file[i].bytes,before.file[i].size,!prior))goto done;}
    if(lc_live_exact(live,argv[7],&before,same_boot)||bg_bound_exact(op,guard,(size_t)gn)||lc_service_exact(op,argv[6])||
       bg_record_exact(op,"platform-legacy-control.json",binding,(size_t)bn)||bg_record_exact(base,"snapshot.json",snapshot,size)||
       migration_record(base,"staged.receipt",receipt,(size_t)rn,!prior)||lc_directory_exact(base,&before)||migration_sync_directory(directory,base))goto done;
    for(int i=0;i<2;i++)if(before.file[i].present){char file[16];snprintf(file,sizeof file,"file-%d",i);if(bg_record_exact(base,file,before.file[i].bytes,before.file[i].size))goto done;}
    if(bg_record_exact(base,"snapshot.json",snapshot,size)||bg_record_exact(base,"staged.receipt",receipt,(size_t)rn)||
       bg_record_exact(op,"platform-legacy-control.json",binding,(size_t)bn)||bg_bound_exact(op,guard,(size_t)gn)||
       lc_service_exact(op,argv[6])||lc_live_exact(live,argv[7],&before,same_boot))goto done;
    if(emit&&verify)printf("{\"ok\":true,\"phase\":\"LEGACY_CONTROL_VERIFIED\",\"snapshotSha256\":\"%s\",\"oldBootId\":\"%s\",\"currentBootId\":\"%s\",\"oldBootEnded\":%s,\"signalsAuthorized\":false,\"serviceStopped\":false,\"activationAllowed\":false}\n",snapshot_sha,input.old_boot,boot,same_boot?"false":"true");
    else if(emit)printf("{\"ok\":true,\"phase\":\"LEGACY_CONTROL_STAGED\",\"snapshotSha256\":\"%s\",\"signalsAuthorized\":false,\"serviceStopped\":false,\"activationAllowed\":false}\n",snapshot_sha);
    result=0;
done:
    if(lc_retired>=0)close(lc_retired);lc_retired=-1;lc_new_generation=0;
    if(op>=0)close(op);if(mig>=0)close(mig);if(live>=0)close(live);if(base>=0)close(base);lc_free(&before);free(input.intent);free(snapshot);
    return result?migration_error("LEGACY_CONTROL_EVIDENCE_UNCONFIRMED"):0;
}
static int legacy_control_stage_main(int argc,char **argv){return legacy_control_impl(argc,argv,1);}

static int lc_retirement_prefix(char *out,size_t capacity,const struct bg_input *input,const char *migration_sha,const char *native,const char *snapshot_sha){
    int n=snprintf(out,capacity,"BROray-legacy-retirement/1\n%s\n%s\n%s\n%s\n%s\n%s\n",input->operation,input->nonce,migration_sha,native,input->old_boot,snapshot_sha);
    return n>0&&(size_t)n<capacity?n:-1;
}
/* The original full executor identity is historical evidence only. No PID is
 * adopted or signalled. The binding also pins every byte of the intent. */
static int lc_retirement_open(int op,const char *op_path,const struct bg_input *input,const char *migration_sha,const char *native){
    int present=bg_exists(op,"platform-legacy-retirement"),bound=bg_exists(op,"platform-legacy-retirement.json");
    if(present==0&&bound==0)return -1;if(present!=1||bound!=1)return -2;
    char path[PATH_MAX],prefix[768],binding[768];int base=-1,objects=-1,result=-2;
    struct migration_file original,intent;memset(&original,0,sizeof original);memset(&intent,0,sizeof intent);
    if(snprintf(path,sizeof path,"%s/platform-legacy-retirement",op_path)>=(int)sizeof path)goto done;
    base=checked_directory(path);if(base<0||migration_read(op,"platform-legacy-control/snapshot.json",&original,0)||original.mode!=0600||
       migration_read(base,"intent.record",&intent,0)||intent.mode!=0600||memchr(intent.bytes,0,intent.size))goto done;
    int n=lc_retirement_prefix(prefix,sizeof prefix,input,migration_sha,native,original.sha);
    if(n<0||intent.size<=(size_t)n||memcmp(intent.bytes,prefix,(size_t)n))goto done;
    const char *executor=intent.bytes+n;long pid=0;unsigned long long start=0;char born[64];int used=0;
    if(sscanf(executor,"{\"pid\":%ld,\"startTicks\":\"%llu\",\"bootId\":\"%63[^\"]\",\"executable\":%n",&pid,&start,born,&used)!=3||pid<=1||pid>INT_MAX||!start||!used||strlen(born)!=36||!token(born,36)||!strcmp(born,input->old_boot))goto done;
    if(intent.bytes[intent.size-1]!='\n'||memchr(executor,'\n',intent.size-(size_t)n-1))goto done;
    n=snprintf(binding,sizeof binding,"{\"schemaVersion\":1,\"contract\":\"broray-legacy-retirement/1\",\"operationId\":\"%s\",\"stopNonce\":\"%s\",\"intentSha256\":\"%s\",\"snapshotSha256\":\"%s\",\"nativeSha256\":\"%s\"}\n",input->operation,input->nonce,intent.sha,original.sha,native);
    if(n<0||n>=(int)sizeof binding||bg_record_exact(op,"platform-legacy-retirement.json",binding,(size_t)n)||
       snprintf(path,sizeof path,"%s/platform-legacy-retirement/objects",op_path)>=(int)sizeof path)goto done;
    objects=checked_directory(path);if(objects<0)goto done;result=objects;objects=-1;
done:
    if(base>=0)close(base);if(objects>=0)close(objects);free(original.bytes);free(intent.bytes);return result;
}

static int lc_executor_exact(int base,const char *sha,const char *prefix,size_t prefix_size){
    if(!hex64(sha))return -1;char name[96];snprintf(name,sizeof name,"executor-%s.json",sha);
    struct migration_file file;if(migration_read(base,name,&file,0))return -1;
    int bad=file.mode!=0600||strcmp(file.sha,sha)||file.size<=prefix_size||memcmp(file.bytes,prefix,prefix_size);
    free(file.bytes);return bad?-1:0;
}
static int lc_retirement_inventory(int base,const char *prefix,size_t prefix_size,char out[65]){
    DIR *d=directory_stream(base);if(!d)return -1;struct dirent *e;char hashes[256][65];unsigned count=0;int bad=0;errno=0;
    while((e=readdir(d))){const char *name=e->d_name;if(!strcmp(name,".")||!strcmp(name,".."))continue;
        if(!strcmp(name,"intent.record")||!strcmp(name,"objects")||!strcmp(name,"stopped.receipt")||!strcmp(name,"stopped.anchor"))continue;
        int allowed=0;for(int i=0;i<3;i++){char n[32];snprintf(n,sizeof n,"move-%d.intent",i);if(!strcmp(n,name))allowed=1;snprintf(n,sizeof n,"move-%d.done",i);if(!strcmp(n,name))allowed=1;}if(allowed)continue;
        if(strlen(name)!=78||strncmp(name,"executor-",9)||strcmp(name+73,".json")||count==256){bad=1;break;}
        memcpy(hashes[count],name+9,64);hashes[count][64]=0;
        if(lc_executor_exact(base,hashes[count],prefix,prefix_size)){bad=1;break;}count++;errno=0;
    }
    if(!e&&errno)bad=1;closedir(d);if(bad||!count)return -1;
    for(unsigned i=0;i<count;i++)for(unsigned j=i+1;j<count;j++)if(strcmp(hashes[i],hashes[j])>0){char h[65];memcpy(h,hashes[i],65);memcpy(hashes[i],hashes[j],65);memcpy(hashes[j],h,65);}
    struct gen_sha digest;gen_sha_init(&digest);for(unsigned i=0;i<count;i++)gen_sha_add(&digest,hashes[i],65);gen_sha_end(&digest,out);return 0;
}

/* All mutations execute in this one native process under the caller's held
 * installation flock. The read-only coordinator has already joined/reaped.
 * Moves never replace a pathname; displaced metadata remains inspectable. */
static int legacy_retirement_apply(char **argv,int op,const char *op_path,const struct bg_input *input,const char *native,const struct identity *executor,int held,int statefd,const char *shell,const char *controller,int verify_only){
    int live=-1,base=-1,objects=-1,result=75,prior=0;struct lc_inventory expected;memset(&expected,0,sizeof expected);
    struct migration_file original,service,intent;memset(&original,0,sizeof original);memset(&service,0,sizeof service);memset(&intent,0,sizeof intent);
    char path[PATH_MAX],prefix[768],binding[768],intent_sha[65],executor_sha[65],record_name_[96],inventory[65],terminal[256];
    char *owner=NULL,*execution=NULL;size_t execution_size=0;
    if(!strcmp(boot,input->old_boot)||migration_read(op,"platform-legacy-control/snapshot.json",&original,0)||original.mode!=0600||
       migration_read(op,"platform-service.json",&service,0)||service.mode!=0600)goto done;
    const char *begin=strstr(original.bytes,"\"oldOwner\":");if(!begin)goto done;begin+=11;const char *end=strstr(begin,",\"entries\":[");if(!end||end<=begin)goto done;
    owner=strndup(begin,(size_t)(end-begin));if(!owner)goto done;
    int pn=lc_retirement_prefix(prefix,sizeof prefix,input,argv[4],native,original.sha);if(pn<0)goto done;
    if(snprintf(path,sizeof path,"%s%sopt/var/lib/broray-updater",argv[2],strcmp(argv[2],"/")?"/":"")>=(int)sizeof path)goto done;
    live=checked_directory(path);if(live<0)goto done;
    /* A legacy migration cannot coexist with an unclassified generation. */
    int has_generation=bg_exists(live,"generations");if(has_generation<0)goto done;
    if(has_generation){
        if(!verify_only||lc_generation_proof(argv,op,op_path,input,native))goto done;
        lc_new_generation=1;
    }
    lc_retired=lc_retirement_open(op,op_path,input,argv[4],native);if(lc_retired==-2)goto done;
    if(lc_read(live,owner,&expected))goto done;
    if(lc_retired>=0){close(lc_retired);lc_retired=-1;prior=1;}
    if(verify_only&&!prior)goto done;
    if(snprintf(path,sizeof path,"%s/platform-legacy-retirement",op_path)>=(int)sizeof path)goto done;
    FILE *f=open_memstream(&execution,&execution_size);if(!f)goto done;
    fwrite(prefix,1,(size_t)pn,f);identity_json(f,executor);fputc('\n',f);if(fclose(f))goto done;digest_bytes(execution,execution_size,executor_sha);
    if(!prior){
        if(bg_exists(op,"platform-legacy-retirement.json")!=0||mkdirat(op,"platform-legacy-retirement",0700)||fsync(op))goto done;
        base=checked_directory(path);if(base<0||bg_empty(base)||migration_record(base,"intent.record",execution,execution_size,1)||
           mkdirat(base,"objects",0700)||fsync(base))goto done;
        strcpy(intent_sha,executor_sha);
        int n=snprintf(binding,sizeof binding,"{\"schemaVersion\":1,\"contract\":\"broray-legacy-retirement/1\",\"operationId\":\"%s\",\"stopNonce\":\"%s\",\"intentSha256\":\"%s\",\"snapshotSha256\":\"%s\",\"nativeSha256\":\"%s\"}\n",input->operation,input->nonce,intent_sha,original.sha,native);
        if(n<0||n>=(int)sizeof binding||migration_record(op,"platform-legacy-retirement.json",binding,(size_t)n,1))goto done;
    }else{
        base=checked_directory(path);if(base<0||migration_read(base,"intent.record",&intent,0)||intent.mode!=0600)goto done;strcpy(intent_sha,intent.sha);
    }
    objects=lc_retirement_open(op,op_path,input,argv[4],native);if(objects<0)goto done;lc_retired=objects;
    int terminal_exists=bg_exists(base,"stopped.receipt"),terminal_anchor=bg_exists(base,"stopped.anchor");
    if(terminal_exists<0||terminal_anchor<0||terminal_exists!=terminal_anchor)goto done;
    if(verify_only&&!terminal_exists)goto done;
    if(!terminal_exists){
        snprintf(record_name_,sizeof record_name_,"executor-%s.json",executor_sha);
        if(migration_record(base,record_name_,execution,execution_size,1))goto done;
    }
    if(lc_retirement_inventory(base,prefix,(size_t)pn,inventory))goto done;
    for(int i=0;i<3;i++){
        const char *name=i<2?lc_names[i]:"daemon.lock";int present=i<2?expected.file[i].present:expected.lock_present;
        unsigned mode=i<2?expected.file[i].mode:expected.lock_mode;const char *sha=i<2?expected.file[i].sha:"empty-directory";
        char start[32],finish[32],proof[512],writer[65];snprintf(start,sizeof start,"move-%d.intent",i);snprintf(finish,sizeof finish,"move-%d.done",i);
        int started=bg_exists(base,start),completed=bg_exists(base,finish);if(started<0||completed<0||(!started&&completed))goto done;
        strcpy(writer,executor_sha);
        if(started){
            struct migration_file saved;if(migration_read(base,start,&saved,0))goto done;
            const char *last=saved.size>65?saved.bytes+saved.size-65:NULL;
            int bad=saved.mode!=0600||!last||last[64]!='\n';if(!bad){memcpy(writer,last,64);writer[64]=0;bad=!hex64(writer);}free(saved.bytes);if(bad)goto done;
        }
        int n=snprintf(proof,sizeof proof,"BROray-legacy-retire-entry/1\n%s\n%d\n%d\n%04o\n%s\n%s\n",intent_sha,i,present,mode,sha,writer);
        if(n<0||n>=(int)sizeof proof||lc_executor_exact(base,writer,prefix,(size_t)pn)||
           lc_live_exact(live,owner,&expected,0))goto done;
        int saved=bg_exists(objects,name),current=lc_new_generation?0:bg_exists(live,name);if(saved<0||current<0||saved+current!=present||(!started&&saved))goto done;
        if(terminal_exists&&(!started||!completed||current))goto done;
        if(migration_record(base,start,proof,(size_t)n,!started&&!terminal_exists))goto done;
        if(present&&current){
            if(completed||syscall(SYS_renameat2,live,name,objects,name,RENAME_NOREPLACE))goto done;
        }
        /* A retry can observe the renamed inode after the first writer died
         * before either directory fsync. Synchronize both sides on that path
         * too, before acknowledging the move in its immutable done record. */
        if(fsync(live)||fsync(objects))goto done;
        if((!lc_new_generation&&bg_exists(live,name)!=0)||bg_exists(objects,name)!=present||lc_live_exact(live,owner,&expected,0)||
           bg_record_exact(base,start,proof,(size_t)n)||migration_record(base,finish,proof,(size_t)n,!completed&&!terminal_exists))goto done;
    }
    if(lc_live_exact(live,owner,&expected,0)||lc_retirement_inventory(base,prefix,(size_t)pn,inventory)||
       (lc_new_generation?lc_generation_proof(argv,op,op_path,input,native):bg_exists(live,"generations")!=0)||
       (!verify_only&&recovery_context_proof(argv,held,statefd,op_path,shell,controller,input)))goto done;
    int n=snprintf(terminal,sizeof terminal,"BROray-legacy-stopped/1\n%s\n%s\n%s\n",intent_sha,original.sha,inventory);
    if(n<0||n>=(int)sizeof terminal)goto done;
    char terminal_sha[65],terminal_binding[128];digest_bytes(terminal,(size_t)n,terminal_sha);
    int an=snprintf(terminal_binding,sizeof terminal_binding,"BROray-legacy-stopped-anchor/1\n%s\n",terminal_sha);
    if(an<0||an>=(int)sizeof terminal_binding||
       migration_record(base,"stopped.anchor",terminal_binding,(size_t)an,!terminal_exists)||
       migration_record(base,"stopped.receipt",terminal,(size_t)n,!terminal_exists)||
       bg_record_exact(base,"stopped.anchor",terminal_binding,(size_t)an)||
       bg_record_exact(base,"stopped.receipt",terminal,(size_t)n)||fsync(objects)||fsync(base)||fsync(op)||fsync(live)||
       (!verify_only&&recovery_context_proof(argv,held,statefd,op_path,shell,controller,input)))goto done;
    if(!verify_only)printf("{\"ok\":true,\"phase\":\"STOPPED\",\"serviceStopped\":true,\"activationAllowed\":false,\"signalsSent\":false,\"replayed\":%s}\n",terminal_exists?"true":"false");result=0;
done:
    lc_retired=-1;lc_new_generation=0;if(objects>=0)close(objects);if(base>=0)close(base);if(live>=0)close(live);
    free(original.bytes);free(service.bytes);free(intent.bytes);free(owner);free(execution);lc_free(&expected);
    return result?migration_error("LEGACY_RETIREMENT_UNCONFIRMED"):0;
}
