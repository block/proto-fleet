WITH objects AS (
 SELECT c.oid, c.relname, c.relkind, c.relrowsecurity, c.relforcerowsecurity, c.reloptions
 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname='public' AND c.relname<>'schema_migrations'
 AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.classid='pg_class'::regclass AND d.objid=c.oid AND d.deptype='e')
), entries AS (
 SELECT 'relation/'||relname AS key, jsonb_build_array(relkind,relrowsecurity,relforcerowsecurity,reloptions) AS definition FROM objects WHERE relkind IN ('r','p','v','m','S')
 UNION ALL
 SELECT 'column/'||c.relname||'/'||a.attname, jsonb_build_array(format_type(a.atttypid,a.atttypmod),a.attnotnull,pg_get_expr(d.adbin,d.adrelid),a.attidentity,a.attgenerated)
 FROM objects c JOIN pg_attribute a ON a.attrelid=c.oid LEFT JOIN pg_attrdef d ON d.adrelid=c.oid AND d.adnum=a.attnum
 WHERE c.relkind IN ('r','p','v','m') AND a.attnum>0 AND NOT a.attisdropped
 UNION ALL
 SELECT 'constraint/'||c.relname||'/'||x.conname, to_jsonb(pg_get_constraintdef(x.oid,true)) FROM objects c JOIN pg_constraint x ON x.conrelid=c.oid WHERE c.relkind IN ('r','p')
 UNION ALL
 SELECT 'index/'||c.relname||'/'||i.relname, jsonb_build_array(pg_get_indexdef(i.oid),x.indisvalid,x.indisready) FROM objects c JOIN pg_index x ON x.indrelid=c.oid JOIN pg_class i ON i.oid=x.indexrelid WHERE c.relkind IN ('r','p')
 UNION ALL
 SELECT 'view/'||c.relname,to_jsonb(pg_get_viewdef(c.oid,true)) FROM objects c WHERE c.relkind IN ('v','m') AND NOT EXISTS (SELECT 1 FROM timescaledb_information.continuous_aggregates t WHERE t.view_schema='public' AND t.view_name=c.relname)
 UNION ALL
 SELECT 'function/'||p.proname||'('||pg_get_function_identity_arguments(p.oid)||')',jsonb_build_array(pg_get_functiondef(p.oid),p.prosecdef,p.proconfig,EXISTS (SELECT 1 FROM aclexplode(COALESCE(p.proacl,acldefault('f',p.proowner))) acl WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE'))
 FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='public' AND p.prokind IN ('f','p') AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.classid='pg_proc'::regclass AND d.objid=p.oid AND d.deptype='e')
 UNION ALL
 SELECT 'trigger/'||c.relname||'/'||t.tgname,jsonb_build_array(pg_get_triggerdef(t.oid,true),t.tgenabled) FROM objects c JOIN pg_trigger t ON t.tgrelid=c.oid WHERE NOT t.tgisinternal AND t.tgname NOT LIKE 'ts_%'
 UNION ALL
 SELECT 'enum/'||t.typname, jsonb_agg(e.enumlabel ORDER BY e.enumsortorder) FROM pg_type t JOIN pg_namespace n ON n.oid=t.typnamespace JOIN pg_enum e ON e.enumtypid=t.oid WHERE n.nspname='public' GROUP BY t.typname
 UNION ALL
 SELECT 'sequence/'||c.relname,jsonb_build_array(s.seqtypid::regtype::text,s.seqstart,s.seqincrement,s.seqmin,s.seqmax,s.seqcache,s.seqcycle) FROM objects c JOIN pg_sequence s ON s.seqrelid=c.oid
 UNION ALL
 SELECT 'policy/'||tablename||'/'||policyname,jsonb_build_array(permissive,roles,cmd,qual,with_check) FROM pg_policies WHERE schemaname='public'
 UNION ALL
 SELECT 'hypertable/'||hypertable_name,jsonb_build_array(num_dimensions,compression_enabled) FROM timescaledb_information.hypertables WHERE hypertable_schema='public'
 UNION ALL
 SELECT 'dimension/'||hypertable_name||'/'||column_name,jsonb_build_array(column_type,dimension_type,time_interval,integer_interval,integer_now_func, num_partitions) FROM timescaledb_information.dimensions WHERE hypertable_schema='public'
 UNION ALL
 SELECT 'aggregate-dimension/'||a.view_name||'/'||d.column_name,jsonb_build_array(d.column_type,d.dimension_type,d.time_interval,d.integer_interval,d.integer_now_func,d.num_partitions) FROM timescaledb_information.dimensions d JOIN timescaledb_information.continuous_aggregates a ON a.materialization_hypertable_schema=d.hypertable_schema AND a.materialization_hypertable_name=d.hypertable_name WHERE a.view_schema='public'
 UNION ALL
 SELECT 'compression/'||COALESCE(a.view_name,c.hypertable_name)||'/'||c.attname,jsonb_build_array(c.segmentby_column_index,c.orderby_column_index,c.orderby_asc,c.orderby_nullsfirst) FROM timescaledb_information.compression_settings c LEFT JOIN timescaledb_information.continuous_aggregates a ON a.materialization_hypertable_schema=c.hypertable_schema AND a.materialization_hypertable_name=c.hypertable_name WHERE c.hypertable_schema='public' OR a.view_schema='public'
 UNION ALL
 SELECT 'aggregate/'||view_name,jsonb_build_array(hypertable_schema,hypertable_name,materialized_only,compression_enabled,view_definition) FROM timescaledb_information.continuous_aggregates WHERE view_schema='public'
 UNION ALL
 SELECT 'job/'||j.proc_schema||'/'||j.proc_name||'/'||COALESCE(a.view_name,j.hypertable_name,''), jsonb_agg(j.config-'hypertable_id'-'mat_hypertable_id' ORDER BY (j.config-'hypertable_id'-'mat_hypertable_id')::text)
 FROM timescaledb_information.jobs j LEFT JOIN timescaledb_information.continuous_aggregates a ON a.materialization_hypertable_name=j.hypertable_name AND a.materialization_hypertable_schema=j.hypertable_schema
 WHERE j.hypertable_schema='public' OR a.view_schema='public' OR j.proc_schema='public'
 GROUP BY j.proc_schema,j.proc_name,COALESCE(a.view_name,j.hypertable_name,'')
)
SELECT COALESCE(jsonb_object_agg(key,definition ORDER BY key),'{}'::jsonb) FROM entries;
