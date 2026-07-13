The recent change to add username and project to edict keys went too far by requiring tools to accept these parameters. A Court instance is scoped to a single (username, project) context, so tools should use the stored context rather than requiring parameters.

**Changes needed:**

1. **Remove username/project from tool parameters:**
   - `get_edict_status`: Remove Username/Project from Call() params, use MinisterBase context
   - `update_edict`: Remove Username/Project from Call() params, use MinisterBase context  
   - `request_zhengming`: Remove Username/Project from Call() params, use MinisterBase context
   - Fix parameter schemas to match

2. **Fix transition_edict tool:**
   - Currently queries by `edict_id` only - will fail with composite PK
   - Needs to use stored username/project context
   - Update all queries to use composite key

3. **Update all database queries:**
   - Ensure all tool queries use `WHERE edict_id = ? AND username = ? AND project = ?`
   - Use stored username/project from MinisterBase/Court context

4. **Test fixes:**
   - Update test helpers to use proper EdictKey construction
   - Ensure all tests pass with composite PK

**Design principle:** A Court works within a specific context. Tools inherit this context rather than requiring explicit parameters for every call.

Evidence: - court/court.go:320: `EdictKey()` method constructs keys from config
- court/minister.go:206: MinisterBase now stores username and project fields
- court/tools/edict.go: Parameter schemas don't include username/project but Call() methods accept them
- court/tools/zhengming.go: Same inconsistency
- storage/court_schema.go: Edict has composite primary key (edict_id, username, project)
- missing-169.log:244: Shows "Edict not found" error when username/project are empty
