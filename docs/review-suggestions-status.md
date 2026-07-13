# Review Suggestions - Status Report

## Issue
The review ritual was executed and printed suggestions for improvements, but those suggestions cannot be found using asimisql.

## Root Cause Analysis

### 1. Database Corruption
The Court database at `~/.local/share/asimi/asimi.sqlite` was corrupted (0 bytes, empty). This prevented any data from being persisted, including:
- Censor precedents (review suggestions)
- Judge verdicts
- Forge manifests
- Edict status updates

The corruption was caused by a "database disk image is malformed" error during initialization.

**Action Taken**: Removed the corrupted database file. It will be recreated on next application start.

### 2. Review Ritual Design
The review ritual's censor step uses ad-hoc diff review (`ReviewDiff`) which does NOT persist findings to the `censor_precedents` table. Only `ReviewDiffWithManifests` persists precedents.

## How to Use asimisql to Find Review Suggestions

Once the database is working and precedents are being recorded, use these queries:

### Find All Recent Review Suggestions
```sql
SELECT precedent_id, manifest_id, principle, ruling, justification, created_at 
FROM censor_precedents 
ORDER BY created_at DESC 
LIMIT 10;
```

### Find Rejected Items (Issues to Fix)
```sql
SELECT principle, justification 
FROM censor_precedents 
WHERE ruling = 'rejected' 
ORDER BY created_at DESC;
```

### Find All Precedents for a Specific Edict
```sql
SELECT cp.*, fm.file_path 
FROM censor_precedents cp
JOIN forge_manifests fm ON cp.manifest_id = fm.manifest_id
WHERE fm.edict_id = 'edict-XXXXX'
ORDER BY cp.created_at DESC;
```

## Recommended Fix

To ensure review suggestions are persisted:

1. **Modify the review ritual** to use `ReviewDiffWithManifests` instead of ad-hoc review
2. **Or** add a "then" step to the censor phase that explicitly logs precedents
3. **Ensure database is healthy** before running rituals

## Current Status

- ✅ Updated `asimisql` tool description with example queries for finding review suggestions
- ✅ Removed corrupted database file
- ⚠️ Review suggestions from the previous session are **lost** (were never persisted)
- ⚠️ Need to re-run the review ritual after database is reinitialized

## Next Steps

1. Restart asimi to reinitialize the database
2. Re-run the review ritual: `:ritual review`
3. Use asimisql to query the censor_precedents table for suggestions
4. Consider improving the review ritual to ensure precedents are always recorded
