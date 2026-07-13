Strategic Plan
==============

1. **兵部 Strategist Report: Censor-to-Sage Migration Plan**

This refactoring requires careful sequencing to preserve institutional memory (precedent table) while eliminating redundancy. The dependency graph must ensure no functionality is lost before the Censor is removed.

---

## Dependency Graph (DAG)

```
Ling 1 (Examine Censor)
    ↓
Ling 2 (Migrate Methods to Confucius)
    ↓
Ling 3 (Update Confucius Prompt & Tools)
    ↓
Ling 4 (Remove Censor from Court)
    ↓
Ling 5 (Update Ritual YAMLs)
    ↓
Ling 6 (Delete censor.go)
```

---

## Executable Ling Orders

insert_ling(id="ling_01_examine_censor", title="Examine Censor Implementation", description="Read court/censor.go to catalog all methods, tools, and database functions that must be migrated to Confucius. Document: ReviewDiff, ReviewDiffWithManifests, LogPrecedent, QueryPrecedentsByPrinciple, GetQuenchedManifests, and all Tool definitions.", dependencies=[], testable="Output a list of all Censor methods and tools with their signatures")

insert_ling(id="ling_02_migrate_to_confucius", title="Migrate Censor Methods to Confucius", description="Copy all Censor review methods and database functions to court/confucius.go. Include: ReviewDiff, ReviewDiffWithManifests, LogPrecedent, QueryPrecedentsByPrinciple, GetQuenchedManifests, and tool structs (RecordPrecedentTool, QueryPrecedentsTool, ReviewDiffTool, ListQuenchedManifestsTool).", dependencies=["ling_01_examine_censor"], testable="confucius.go compiles with all Censor methods present; grep confirms method signatures exist")

insert_ling(id="ling_03_update_confucius_metadata", title="Update Confucius SystemPrompt and Tools Registry", description="Update ConfuciusRole constant to include code review responsibilities. Update Confucius.SystemPrompt to mention 正名 AND code review. Update Confucius.Tools() method to return the migrated precedent/review tools.", dependencies=["ling_02_migrate_to_confucius"], testable="Confucius.Tools() returns ReviewDiffTool, RecordPrecedentTool, QueryPrecedentsTool; SystemPrompt contains 'review' keyword")

insert_ling(id="ling_04_remove_censor_instantiation", title="Remove Censor from Court Minister Registry", description="Edit court/court.go line 120 to remove: s.ministers[\"censor\"] = NewCensor(...). Ensure court compiles with 5 ministers instead of 6.", dependencies=["ling_02_migrate_to_confucius"], testable="court.go compiles; grep confirms no NewCensor instantiation; minister count is 5")

insert_ling(id="ling_05_update_ritual_yaml", title="Update Ritual YAML References", description="Update builtin_rituals.yaml to replace all \"censor\" minister references with \"confucius\" in castle-siege and review rituals. Verify YAML syntax remains valid.", dependencies=["ling_04_remove_censor_instantiation"], testable="grep -v 'censor' on ritual YAMLs returns no minister references; YAML parses without error")

insert_ling(id="ling_06_delete_censor_file", title="Delete Censor Source File", description="Delete court/censor.go after all migration is verified complete. This is the final step - do not execute until all prior lings pass tests.", dependencies=["ling_02_migrate_to_confucius", "ling_03_update_confucius_metadata", "ling_04_remove_censor_instantiation", "ling_05_update_ritual_yaml"], testable="court/censor.go does not exist; full test suite passes; court builds successfully")

---

## Critical Constraints

| Constraint | Status |
|------------|--------|
| storage.CensorPrecedent table | **KEEP AS-IS** (do not rename - preserves existing data) |
| Minister count | 6 → 5 |
| Precedent functionality | Must remain fully operational |
| Deletion order | censor.go deleted LAST (ling_06) |

---

**兵部 Seal:** This plan ensures zero data loss while consolidating responsibilities. The Sage now holds both 正名 and review authority - a more unified institutional memory. Execute in sequence.
