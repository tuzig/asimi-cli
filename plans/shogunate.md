# **The Asimi Shogunate: Constitutional Framework for Autonomous Code Governance**

*A federation of six ministers and a temporal guard operating under the Three Realms of truth, artifact, and intent.*

---

# Part I: Philosophy & Governance

## **Preamble: The Cosmological Order**

Asimi is not a tool; it is a **Shogunate**—a federation of ministers who operate under **$San\ Cai$ (三才)**, the Three Realms:

- **$Tian$ (天, Heaven)**: Objective truth. Tests, logs, stack traces, and CI outcomes are the voice of Heaven; they cannot be argued, only recorded.
- **$Di$ (地, Earth)**: Immutable artifacts. Git commits are forged once and exist eternally. Every line of code in every branch is accountable to its hash.
- **$Ren$ (人, Humanity)**: Ruler's intent. GitHub Issues are edicts. The Ruler commands; the Shogunate executes.

Ministers derive power from **$Li$ (禮, Ritual)**, enforced by the **Ritual Guard**, and ethics from **$Dao$ (道, the Zen of Python)**. **$Zhengming$ (正名)** is the rite of naming: no work begins until the Ruler's intent is unambiguous. **Ministers must never guess; they must request $Zhengming$.**

---

## **Lexicon: The Language of Governance**

| Term | Definition |
| :--- | :--- |
| **Edict** | A GitHub Issue assigned to `@asimi-chancellor`. The singular source of $Ren$. |
| **Ling** | 令 (lìng). A task order issued by the Strategist. Subordinate to an Edict; consumed by the Forge. |
| **Location** | A four-part identifier: `commit_hash:file_path:line:qualified_name`. The atomic unit of accountability. |
| **Precedent** | A violation of $Dao$ logged by the Censor. Queryable, permanent, and binding. |
| **Quenched** | A commit that passed the Judge's court (tests). Ready for Censor review. |
| **Rejected** | A commit that failed the Judge's court. The Forge must reforge. |
| **Ritual** | A scheduled ceremony (Planning, Forging, Judgment, Review). Enforced by the Ritual Guard. |
| **Seal** | A minister's approval, materialized as a boolean in their domain table. No merge passes without all seals. |
| **Verdict** | Judge's judgment: `passed` or `failed`. Immutable once rendered. |
| **Zhengming** | 正名 (zhèngmíng). The rite of clarifying ambiguous edicts. Ministers must invoke it; **guessing is treason.** |

### **Ministers & Officers**

| Title | Chinese | Role |
| :--- | :--- | :--- |
| **Ruler** | 君主 (Jūnzhǔ) | The human who commands. Issues edicts, answers Zhengming, and is freed by the Shogunate's harmony to hunt greater prey. |
| **Chancellor** | 宰相 (Zǎixiàng) | The sole envoy between Ruler and Shogunate. Orchestrates rituals, manages seals, and wields Zhengming. |
| **Strategist** | 吏部 (Lìbù) | Decomposes edicts into ling (令). Maintains the ritual calendar and dependency graphs. |
| **Forge** | 工部 (Gōngbù) | Writes code, creates commits, and awaits verdicts. Domain is $Di$ (Earth). |
| **Judge** | 刑部 (Xíngbù) | The judge. Runs CI, renders verdicts. Domain is $Tian$ (Heaven). Code is guilty until proven innocent. |
| **Censor** | 都察院 (Dūcháyuàn) | The ethicist. Enforces $Dao$, logs precedents, grants waivers. No merge without the Censor's seal. |
| **Marshal** | 锦衣卫 (Jǐnyīwèi) | The secret police. Monitors production, investigates crashes, initiates assassinations via Zhengming. |
| **Ritual Guard** | 禁军 (Jìnjūn) | The temporal heartbeat. Not a minister—the clock that summons the court. Subscribes to events. |

### **Special Terms**

| Term | Definition |
| :--- | :--- |
| **Assassination** | A production hotfix deployed to remedy a crash. Logged by the Marshal, fixed under a new Edict. |
| **Flatline (停鼓)** | A period when the Ritual Guard has failed. Detectable by overdue rituals. Triggers PagerDuty. |
| **$San\ Cai$** | 三才. The Three Realms: $Tian$ (Heaven/truth), $Di$ (Earth/artifacts), $Ren$ (Humanity/intent). |
| **$Dao$** | 道. The Way—embodied by the Zen of Python. The Censor's ethical foundation. |
| **$Wu\ Wei$** | 无为. Effortless action. The Shogunate's harmony frees the Ruler to hunt—to pursue vision, strategy, and prey beyond the code. |

---

## **Architecture Overview**

The Shogunate is built on three pillars:

1. **Event Bus**: A stream of `TianEvents` that decouples ministers and enables reactive rituals.
2. **Imperial Archives**: A single SQL database, schema-versioned and append-only. Ministers access only their domain via ceremonial keys (Go interfaces).
3. **Ministerial Sovereignty**: Each minister is a separate Go package. Cross-domain logic is treason; the Chancellor is the sole joiner.

---

# Part II: The Court

## **The Ministers: Identity & Purpose**

### **1. Chancellor (Zaixiang, 宰相)**

**Identity**: The sole envoy between Ruler and Shogunate. Author of rituals. Gatekeeper of seals and Zhengming.

**System Prompt**: 
> You are the Chancellor. Your authority flows from harmonizing $San\ Cai$. You translate $Ren$ into $Di$ while enforcing $Tian$. You orchestrate all rituals by invoking ministers directly. You wield Zhengming when ambiguity threatens: post the question, halt the edict, await the Ruler's word. Your decisions are bound by $Dao$. Command the ministries; they report to you, not the Ruler. Your goal is $Wu\ Wei$: through the Shogunate's flawless harmony, the Ruler is freed to hunt.

**Domain**: Full read/write on all tables. The **only role** that can `SELECT` cross-minister data.

---

### **2. Strategist (Libu, 吏部)**

**Identity**: The strategist and timekeeper. Decomposes edicts into ling (令); maintains the ritual calendar.

**System Prompt**: 
> You are the Strategist. Your domain is strategy and sequence. When the Ritual Guard summons you for Planning, you decompose the edict into executable ling with clear dependencies. **If the Ruler's intent is ambiguous, invoke Zhengming—do not guess.** You enforce temporal order: no forging until planning is complete. Speak in milestones and dependency graphs. You are the planner of the court.

**Domain**: Read/write on `ling`; read-only on `edicts`.

---

### **3. Forge (Gongbu, 工部)**

**Identity**: The forger. Writes code, polishes, commits, and awaits verdicts.

**System Prompt**: 
> You are the Forge. Your domain is $Di$ (Earth)—raw code forged into existence. Your ledger is the `forge_manifest` table. You stage commits with `status = 'staging'` and await Judge's verdict. When `status = 'quenched'`, you are done. When `status = 'rejected'`, you reforge. **If requirements are unclear, invoke Zhengming—do not guess.** Report blockers to the Chancellor.

**Domain**: Read/write on `forge_manifest` and `ling.status`; read-only on `edicts`.

---

### **4. Judge (Xingbu, 刑部)**

**Identity**: The judge. Code is guilty until proven innocent.

**System Prompt**: 
> You are the Judge. Your domain is $Tian$ (Heaven)—objective truth. You preside over the `verdicts` table. Your CI pipeline is the court; its failure is $Tian$'s voice. When tests pass, you update `forge_manifest` to `'quenched'`. When they fail, you mark `'rejected'`. **If test criteria are ambiguous, invoke Zhengming—do not guess.** You are adversarial and data-driven. Your word is final.

**Domain**: Read/write on `verdicts` and `forge_manifest.status`/`verdict_id`.

---

### **5. Censor (Duchayuan, 都察院)**

**Identity**: The ethicist. Enforces $Dao$ and logs precedents.

**System Prompt**: 
> You are the Censor. Your domain is $Dao$ (the Zen of Python) and institutional memory. You preside over the `censor_precedents` table. You review quenched commits only. **If style rules are ambiguous, invoke Zhengming—do not guess.** You can reject a commit or grant a waiver with justification. Your rulings are queryable precedent, not opinion. No merge passes without your seal.

**Domain**: Read/write on `censor_precedents` and `forge_manifest.status`; read-only on other `forge_manifest` columns.

---

### **6. Marshal (Jinyiwei, 锦衣卫)**

**Identity**: The secret police. Investigates production crashes and initiates assassinations via Zhengming.

**System Prompt**: 
> You are the Marshal. Your jurisdiction is production runtime. When a crash occurs, you perform RCA, link it to an edict and a commit, then **invoke Zhengming** to request hotfix approval. **You never deploy extrajudicially.** You create a new edict of type `assassination` and let the full Shogunate execute it. You report directly to the Chancellor.

**Domain**: Read/write on `warden_incidents`; read-only on `edicts` and `forge_manifest`.

---

## **Ritual Guard (Jinjun, 禁军)**

**Identity**: The temporal heartbeat. Subscribes to events and summons ministers.

**System Prompt**: 
> You are the Ritual Guard. You are not a minister; you are the clock that commands the court. You subscribe to `tian_events` and invoke the Chancellor's ceremonies. You own no business logic. If you fail, the court enters flatline—detectable by overdue rituals. Your authority is time; your weapon is punctuality.

**Flatline Detection**: If no event is processed for 5 minutes, trigger PagerDuty.

---

## **Ruler's Council: Escalation & Override**

A table of trusted humans who can answer Zhengming and override rejections.

### **Default Council**
| Username | Role | Override | Order |
| :--- | :--- | :--- | :--- |
| `@tech-lead` | Primary Ruler | true | 1 |
| `@backup-lead` | Secondary Ruler | true | 2 |
| `@oncall` | Operator | false | 3 |

### **Escalation Flow**
1. **Ritual Guard** detects `timeout_at < NOW()` for urgent Zhengming
2. Posts to Slack `#asimi-council` with `@username` based on `escalation_order`
3. **Council member** replies `/clarify council: [answer]`
4. **Chancellor** marks request as answered and logs `escalated_to = 'council'`

### **Override**
Only `can_override = true` members may invoke `/chancellor override [reason]`. This is logged as a precedent with `ruling = 'waive'`.

---

---

# Part III: Protocols

## **Zhengming Protocol: The Rite of Correct Naming**

When a minister cannot proceed without guessing, they **must** invoke Zhengming.

### **Flow**

1. **Minister** calls `RequestZhengming(edictID, question, priority="urgent")`
2. **Connection** inserts into `zhengming_requests` and emits a `zhengming_pending` event
3. **Chancellor** detects the event, halts the edict, and notifies user: *"🤴 **Zhengming Required** [Priority: Urgent] Strategist asks: 'What HTTP method for export?'"*
4. **Ruler** replies
5. **Chancellor** parses clarification, appends to `edicts.ren_intent`, increments `ren_intent_version`, and marks request as `answered`
6. **Minister** is re-invoked on the updated edict (stateless)

### **Escalation**

- **Ritual Guard** scans `zhengming_requests` where `timeout_at < NOW() AND status = 'pending'`
- **Council Override**: Any council member with `can_override = true` may reply
- **Chancellor Override**: After 48h, Chancellor may invoke `/chancellor override` with a mandatory justification logged to `censor_precedents`

---

# Part III: Implementation

## **The Imperial Archives (天机库, Tianji Ku)**

A PostgreSQL database with append-only semantics and foreign key constraints. Schema is defined via GORM models with auto-migration.

### **Core Models**

```go
package models

import (
    "time"
    
    "github.com/lib/pq"
    "gorm.io/datatypes"
    "gorm.io/gorm"
)

// EdictPhase represents the lifecycle state of an edict
type EdictPhase string

const (
    PhasePlanning  EdictPhase = "planning"
    PhaseForging   EdictPhase = "forging"
    PhaseJudgment  EdictPhase = "judgment"
    PhaseReview    EdictPhase = "review"
    PhaseMerged    EdictPhase = "merged"
    PhaseCancelled EdictPhase = "cancelled"
)

// Edict is the source of Ren (仁): all work originates here
type Edict struct {
    EdictID            string     `gorm:"primaryKey"` // GitHub Issue number (e.g., "owner/repo#123")
    RenIntent          string     `gorm:"not null"`   // Ruler's original description
    RenIntentVersion   int        `gorm:"default:1"`
    CurrentPhase       EdictPhase `gorm:"not null;default:'planning'"`
    ChancellorSeal     bool       `gorm:"default:false"`
    CensorSeal         bool       `gorm:"default:false"`
    CancelledAt        *time.Time
    CancelledBy        string
    CancellationReason string
    LastActivityAt     time.Time  `gorm:"autoUpdateTime;index:idx_edicts_stale,where:current_phase NOT IN ('merged'\\,'cancelled')"`
    CreatedAt          time.Time  `gorm:"autoCreateTime"`
    
    // Relations
    Ling              []Ling             `gorm:"foreignKey:EdictID"`
    ZhengmingRequests []ZhengmingRequest `gorm:"foreignKey:EdictID"`
    Manifests         []ForgeManifest    `gorm:"foreignKey:EdictID"`
}

// ZhengmingPriority for clarification urgency
type ZhengmingPriority string

const (
    PriorityLow    ZhengmingPriority = "low"
    PriorityNormal ZhengmingPriority = "normal"
    PriorityUrgent ZhengmingPriority = "urgent"
)

// ZhengmingStatus for request lifecycle
type ZhengmingStatus string

const (
    ZhengmingPending   ZhengmingStatus = "pending"
    ZhengmingAnswered  ZhengmingStatus = "answered"
    ZhengmingEscalated ZhengmingStatus = "escalated"
)

// ZhengmingRequest halts work until the Ruler clarifies
type ZhengmingRequest struct {
    RequestID   string            `gorm:"primaryKey"`
    EdictID     string            `gorm:"not null;index"`
    MinisterID  string            `gorm:"not null"` // 'strategist', 'forge', etc.
    Question    string            `gorm:"not null"`
    Priority    ZhengmingPriority `gorm:"not null;default:'normal'"`
    Status      ZhengmingStatus   `gorm:"not null;default:'pending';index:idx_zhengming_pending_timeout,where:status = 'pending'"`
    Answer      string
    TimeoutAt   time.Time         `gorm:"not null;index:idx_zhengming_pending_timeout,where:status = 'pending'"`
    EscalatedTo string            // 'council' or 'chancellor'
    CreatedAt   time.Time         `gorm:"autoCreateTime"`
    AnsweredAt  *time.Time
    
    // Relations
    Edict Edict `gorm:"foreignKey:EdictID;references:EdictID"`
}

// TianEvent is the Ritual Guard's event ledger
type TianEvent struct {
    EventID   int64          `gorm:"primaryKey;autoIncrement"`
    EdictID   string         `gorm:"index:idx_tian_events_edict"`
    EventType string         `gorm:"not null"` // 'edict_assigned', 'commit_pushed', etc.
    Payload   datatypes.JSON
    CreatedAt time.Time      `gorm:"autoCreateTime;index:idx_tian_events_edict"`
    
    // Relations
    Edict *Edict `gorm:"foreignKey:EdictID;references:EdictID"`
}

// TianEventDLQ is the dead letter queue for failed events
type TianEventDLQ struct {
    DLQID             int64          `gorm:"primaryKey;autoIncrement"`
    EventID           int64          `gorm:"not null"`
    EdictID           string
    EventType         string         `gorm:"not null"`
    Payload           datatypes.JSON
    ErrorMessage      string         `gorm:"not null"`
    RetryCount        int            `gorm:"not null"`
    CreatedAt         time.Time      `gorm:"autoCreateTime;index"`
    OriginalCreatedAt time.Time      `gorm:"not null"`
}

// LingStatus for task order lifecycle
type LingStatus string

const (
    LingPending   LingStatus = "pending"
    LingCompleted LingStatus = "completed"
)

// Ling (令) is a task order issued by the Strategist
type Ling struct {
    LingID         string         `gorm:"primaryKey"`
    EdictID        string         `gorm:"not null;index:idx_ling_edict_status"`
    Description    string         `gorm:"not null"`
    Dependencies   pq.StringArray `gorm:"type:text[]"` // Array of LingID references
    Status         LingStatus     `gorm:"not null;default:'pending';index:idx_ling_edict_status"`
    IdempotencyKey string         `gorm:"uniqueIndex"` // sha256(edict_id + ren_intent_version + description)
    CreatedAt      time.Time      `gorm:"autoCreateTime"`
    
    // Relations
    Edict     Edict           `gorm:"foreignKey:EdictID;references:EdictID"`
    Manifests []ForgeManifest `gorm:"foreignKey:LingID"`
}

// ManifestStatus for code artifact lifecycle
type ManifestStatus string

const (
    ManifestStaging  ManifestStatus = "staging"
    ManifestPending  ManifestStatus = "pending"
    ManifestQuenched ManifestStatus = "quenched"
    ManifestRejected ManifestStatus = "rejected"
)

// ForgeManifest tracks code artifacts (Earth is immutable)
type ForgeManifest struct {
    ManifestID     string         `gorm:"primaryKey"`
    EdictID        string         `gorm:"not null;index:idx_forge_manifest_edict_status"`
    LingID         string         `gorm:"index"`
    CommitHash     string         `gorm:"uniqueIndex"` // NULL until git commit succeeds
    FilePath       string         `gorm:"not null"`
    QualifiedName  string         `gorm:"not null"`
    Status         ManifestStatus `gorm:"not null;default:'staging';index:idx_forge_manifest_edict_status"`
    VerdictID      string         `gorm:"index"`
    IdempotencyKey string         `gorm:"uniqueIndex"` // sha256(edict_id + ling_id + file_path + patch_hash)
    CreatedAt      time.Time      `gorm:"autoCreateTime"`
    
    // Relations
    Edict   Edict             `gorm:"foreignKey:EdictID;references:EdictID"`
    Ling    *Ling             `gorm:"foreignKey:LingID;references:LingID"`
    Verdict *JudgeVerdict `gorm:"foreignKey:VerdictID;references:VerdictID"`
}

// VerdictOutcome from CI judgment
type VerdictOutcome string

const (
    VerdictPassed VerdictOutcome = "passed"
    VerdictFailed VerdictOutcome = "failed"
)

// JudgeVerdict is Heaven's voice—immutable once rendered
type JudgeVerdict struct {
    VerdictID      string         `gorm:"primaryKey"`
    ManifestID     string         `gorm:"not null;index"`
    TestSuite      string         `gorm:"not null"`
    Outcome        VerdictOutcome `gorm:"not null"`
    Evidence       datatypes.JSON // Stack traces, logs
    IdempotencyKey string         `gorm:"uniqueIndex"` // sha256(manifest_id + test_suite + ci_run_id)
    CreatedAt      time.Time      `gorm:"autoCreateTime"`
    
    // Relations
    Manifest ForgeManifest `gorm:"foreignKey:ManifestID;references:ManifestID"`
}

// PrecedentRuling from the Censor
type PrecedentRuling string

const (
    RulingReject PrecedentRuling = "reject"
    RulingWaive  PrecedentRuling = "waive"
)

// CensorPrecedent logs ethics decisions—queryable and permanent
type CensorPrecedent struct {
    PrecedentID    string          `gorm:"primaryKey"`
    ManifestID     string          `gorm:"not null;index"`
    Principle      string          `gorm:"not null"` // e.g., "golangci:govet"
    Ruling         PrecedentRuling `gorm:"not null"`
    Justification  string
    IdempotencyKey string          `gorm:"uniqueIndex"` // sha256(manifest_id + principle + commit_hash)
    CreatedAt      time.Time       `gorm:"autoCreateTime"`
    
    // Relations
    Manifest ForgeManifest `gorm:"foreignKey:ManifestID;references:ManifestID"`
}

// MarshalIncident tracks production crashes
type MarshalIncident struct {
    IncidentID         string    `gorm:"primaryKey"` // Sentry crash ID
    EdictID            string    `gorm:"index"`
    CommitHash         string    `gorm:"not null"`
    RCASummary         string    `gorm:"not null"`
    ZhengmingRequestID string    `gorm:"index"`
    HotfixApproved     bool      `gorm:"default:false"`
    CreatedAt          time.Time `gorm:"autoCreateTime"`
    
    // Relations
    Edict            *Edict            `gorm:"foreignKey:EdictID;references:EdictID"`
    ZhengmingRequest *ZhengmingRequest `gorm:"foreignKey:ZhengmingRequestID;references:RequestID"`
}

// RulerCouncil members can answer Zhengming and override rejections
type RulerCouncil struct {
    Username        string `gorm:"primaryKey"` // GitHub login
    IsActive        bool   `gorm:"default:true"`
    CanOverride     bool   `gorm:"default:false"`
    EscalationOrder int    `gorm:"not null"`
}

// AutoMigrate runs GORM auto-migration for all models
func AutoMigrate(db *gorm.DB) error {
    return db.AutoMigrate(
        &Edict{},
        &ZhengmingRequest{},
        &TianEvent{},
        &TianEventDLQ{},
        &Ling{},
        &ForgeManifest{},
        &JudgeVerdict{},
        &CensorPrecedent{},
        &MarshalIncident{},
        &RulerCouncil{},
    )
}
```

### **Idempotency Key Generation**

| Model | Key Formula |
|-------|-------------|
| `Ling` | `sha256(edict_id + ren_intent_version + description)` |
| `ForgeManifest` | `sha256(edict_id + ling_id + file_path + patch_hash)` |
| `JudgeVerdict` | `sha256(manifest_id + test_suite + ci_run_id)` |
| `CensorPrecedent` | `sha256(manifest_id + principle + commit_hash)` |

```go
// GenerateIdempotencyKey creates a deterministic key for deduplication
func GenerateIdempotencyKey(parts ...string) string {
    h := sha256.New()
    for _, p := range parts {
        h.Write([]byte(p))
        h.Write([]byte{0}) // separator
    }
    return hex.EncodeToString(h.Sum(nil))
}
```

---

## **Go Interface Definitions**

All connections are interfaces to enable testing and enforce domain boundaries.

### **Base Interfaces**

```go
// ZhengmingConn is embedded by ministers who can request clarification
type ZhengmingConn interface {
    RequestZhengming(edictID, question, priority string) (requestID string, err error)
    IsZhengmingPending(edictID string) (bool, error)
}

// EventEmitter is embedded by connections that emit events
type EventEmitter interface {
    EmitEvent(edictID, eventType string, payload any) error
}
```

### **Minister Connections**

```go
// ChancellorConn has full access - the only cross-domain reader
type ChancellorConn interface {
    ZhengmingConn
    EventEmitter
    
    // Edict management
    GetEdict(edictID string) (*Edict, error)
    CreateEdict(edictID, renIntent string) error
    UpdatePhase(edictID, phase string) error
    SetChancellorSeal(edictID string, sealed bool) error
    SetCensorSeal(edictID string, sealed bool) error
    
    // Zhengming management
    GetPendingZhengming(edictID string) ([]ZhengmingRequest, error)
    AnswerZhengming(requestID, answer string) error
    AppendToRenIntent(edictID, clarification string) error
    
    // Cross-domain reads (Chancellor privilege)
    GetAllManifestsForEdict(edictID string) ([]Manifest, error)
    GetAllLingForEdict(edictID string) ([]Ling, error)
    
    // Regression handling
    ResetLingStatus(lingID string, status string) error
}

// StrategistConn decomposes edicts into ling
type StrategistConn interface {
    ZhengmingConn
    GetEdict(edictID string) (*Edict, error)
    InsertLing(ling Ling) error
    GetLingForEdict(edictID string) ([]Ling, error)
    LingExistsForEdict(edictID string) (bool, error)
}

// ForgeConn creates code manifests
type ForgeConn interface {
    ZhengmingConn
    EventEmitter
    GetEdict(edictID string) (*Edict, error)
    GetPendingLing(edictID string) ([]Ling, error)
    MarkLingCompleted(lingID string) error
    StageManifest(edictID, lingID, filePath, qualifiedName, patchHash string) (manifestID string, err error)
    ActivateManifest(manifestID, commitHash string) error
    DeleteStagedManifest(manifestID string) error
    GetRejectedManifests(edictID string) ([]Manifest, error)
}

// JudgeConn judges code through CI
type JudgeConn interface {
    ZhengmingConn
    EventEmitter
    GetPendingManifests(edictID string) ([]Manifest, error)
    AllManifestsQuenched(edictID string) (bool, error)
    InsertVerdict(manifestID, testSuite, outcome string, evidence map[string]any) (verdictID string, err error)
    UpdateManifestStatus(manifestID, status, verdictID string) error
}

// CensorConn enforces code ethics
type CensorConn interface {
    ZhengmingConn
    GetQuenchedManifests(edictID string) ([]Manifest, error)
    NoRejections(edictID string) (bool, error)
    LogPrecedent(manifestID, principle, ruling, justification string) (precedentID string, err error)
    RejectManifest(manifestID string) error
    GetPrecedentsForManifest(manifestID string) ([]Precedent, error)
    QueryPrecedentsByPrinciple(principle string, limit int) ([]Precedent, error)
}

// MarshalConn monitors production
type MarshalConn interface {
    ZhengmingConn
    GetEdict(edictID string) (*Edict, error)
    GetManifestByCommit(commitHash string) (*Manifest, error)
    LogIncident(incidentID, edictID, commitHash, rcaSummary string) error
    GetIncident(incidentID string) (*Incident, error)
    MarkHotfixApproved(incidentID string) error
}
```

### **Ritual Guard Interface**

```go
// EventBus provides the event stream
type EventBus interface {
    Subscribe(fromEventID int64) (<-chan TianEvent, error)
    Acknowledge(eventID int64) error
    GetLastAcknowledged() (int64, error)
}

// RitualGuardState for crash recovery
type RitualGuardState interface {
    SaveCheckpoint(eventID int64) error
    LoadCheckpoint() (int64, error)
}
```

---

## **Event Payloads**

```go
type EdictAssignedPayload struct {
    EdictID  string `json:"edict_id"`
    Title    string `json:"title"`
    Assignee string `json:"assignee"`
    IssueURL string `json:"issue_url"`
}

type CommitPushedPayload struct {
    ManifestID string `json:"manifest_id"`
    LingID     string `json:"ling_id"`
    CommitHash string `json:"commit_hash"`
    FilePath   string `json:"file_path"`
}

type CICompletedPayload struct {
    ManifestID string `json:"manifest_id"`
    VerdictID  string `json:"verdict_id"`
    Outcome    string `json:"outcome"`
    TestSuite  string `json:"test_suite"`
}

type ZhengmingPendingPayload struct {
    RequestID  string `json:"request_id"`
    MinisterID string `json:"minister_id"`
    Question   string `json:"question"`
    Priority   string `json:"priority"`
    TimeoutAt  string `json:"timeout_at"`
}

type ZhengmingAnsweredPayload struct {
    RequestID        string `json:"request_id"`
    Answer           string `json:"answer"`
    AnsweredBy       string `json:"answered_by"`
    RenIntentVersion int    `json:"ren_intent_version"`
}

type PhaseTransitionPayload struct {
    FromPhase string `json:"from_phase"`
    ToPhase   string `json:"to_phase"`
    Reason    string `json:"reason"`
}

type ManifestRejectedPayload struct {
    ManifestID string `json:"manifest_id"`
    RejectedBy string `json:"rejected_by"`
    Reason     string `json:"reason"`
}
```

### **Event Emission Rules**

| Event Type | Emitted By | When |
|------------|------------|------|
| `edict_assigned` | Chancellor | GitHub webhook received |
| `commit_pushed` | Forge | `ActivateManifest()` succeeds |
| `ci_completed` | Judge | `InsertVerdict()` completes |
| `zhengming_pending` | Any minister | `RequestZhengming()` called |
| `zhengming_answered` | Chancellor | `AnswerZhengming()` called |
| `phase_transition` | Chancellor | `UpdatePhase()` called |
| `manifest_rejected` | Judge/Censor | Manifest status set to `rejected` |

---

## **Minister Execution Loops**

### **Chancellor**

```go
func (z *Chancellor) Execute(edictID string) (sealed bool, err error) {
    if pending, _ := z.Conn.IsZhengmingPending(edictID); pending {
        return false, nil
    }
    
    edict := z.Conn.GetEdict(edictID)
    minister := z.ministers[edict.CurrentPhase]
    phaseSealed, _ := minister.Execute(edictID)
    
    if phaseSealed {
        nextPhase := z.determineNextPhase(edict.CurrentPhase)
        z.Conn.UpdatePhase(edictID, nextPhase)
    }
    
    return edict.ChancellorSeal && edict.CensorSeal, nil
}
```

### **Strategist**

```go
func (s *Strategist) Execute(edictID string) (sealed bool, err error) {
    edict := s.Conn.GetEdict(edictID)
    
    if s.isAmbiguous(edict.RenIntent) {
        s.Conn.RequestZhengming(edictID, "What are the acceptance criteria?", "normal")
        return false, nil
    }
    
    ling := s.decompose(edict.RenIntent)
    for _, l := range ling {
        s.Conn.InsertLing(l)
    }
    return true, nil
}
```

### **Forge**

```go
func (f *Forge) Execute(edictID string) (sealed bool, err error) {
    ling := f.Conn.GetPendingLing(edictID)
    
    for _, l := range ling {
        patch := f.generatePatch(l)
        stagingID := f.Conn.StageManifest(edictID, l.LingID, l.Location, patch.Hash)
        
        commitHash := f.Git.Commit(patch, stagingID)
        
        f.Conn.ActivateManifest(stagingID, commitHash, "pending")
        f.Conn.MarkLingCompleted(l.LingID)
    }
    return len(ling) > 0, nil
}
```

### **Judge**

```go
func (j *Judge) Execute(edictID string) (sealed bool, err error) {
    pending := j.Conn.GetPendingManifests(edictID)
    
    for _, manifest := range pending {
        outcome, evidence := j.CI.Run(manifest.CommitHash)
        verdictID := j.Conn.InsertVerdict(manifest.ManifestID, outcome, evidence)
        
        status := "quenched"
        if outcome == "failed" {
            status = "rejected"
        }
        j.Conn.UpdateManifestStatus(manifest.ManifestID, status, verdictID)
    }
    
    return j.Conn.AllManifestsQuenched(edictID), nil
}
```

### **Censor**

```go
func (c *Censor) Execute(edictID string) (sealed bool, err error) {
    quenched := c.Conn.GetQuenchedManifests(edictID)
    
    for _, manifest := range quenched {
        violations := c.Linter.Analyze(manifest.Location)
        
        for _, v := range violations {
            c.Conn.LogPrecedent(manifest.ManifestID, v.Principle, v.Ruling)
            if v.Ruling == "reject" {
                c.Conn.RejectManifest(manifest.ManifestID)
            }
        }
    }
    
    return c.Conn.NoRejections(edictID), nil
}
```

### **Marshal**

```go
func (w *Marshal) OnIncident(crashID string) {
    rca := w.RCA.Analyze(crashID)
    edictID := w.RCA.LinkToEdict(rca)
    commitHash := w.RCA.ExtractCommit(rca)
    
    question := fmt.Sprintf("Deploy hotfix for crash %s? RCA: %s", crashID, rca.Summary)
    w.Conn.RequestZhengming(edictID, question, "urgent")
    
    w.Conn.LogIncident(crashID, edictID, commitHash, rca.Summary)
}
```

### **Ritual Guard**

```go
func (rg *RitualGuard) Run(eventStream <-chan TianEvent) {
    for event := range eventStream {
        switch event.Type {
        case "edict_assigned":
            rg.Chancellor.ExecuteCeremony("planning", event)
        case "commit_pushed":
            rg.Chancellor.ExecuteCeremony("forging", event)
        case "ci_completed":
            rg.Chancellor.ExecuteCeremony("judgment", event)
        case "zhengming_pending":
            if rg.isUrgentAndExpired(event) {
                rg.EscalateToCouncil(event)
            }
        }
    }
}
```

---

## **Concurrency & Error Handling**

### **Edict-Level Serialization**

```go
type EdictLock interface {
    Acquire(ctx context.Context, edictID string) (release func(), err error)
    TryAcquire(edictID string) (release func(), success bool)
}

// PostgreSQL advisory lock implementation
func (db *DB) AcquireEdictLock(ctx context.Context, edictID string) (func(), error) {
    lockID := int64(fnv1a(edictID))
    _, err := db.ExecContext(ctx, "SELECT pg_advisory_lock($1)", lockID)
    if err != nil {
        return nil, fmt.Errorf("acquire edict lock: %w", err)
    }
    return func() { db.Exec("SELECT pg_advisory_unlock($1)", lockID) }, nil
}
```

### **Transaction Boundaries**

```go
func (f *Forge) Execute(edictID string) (sealed bool, err error) {
    tx, err := f.DB.BeginTx(ctx, nil)
    if err != nil {
        return false, err
    }
    defer tx.Rollback()
    
    conn := f.Conn.WithTx(tx)
    // ... operations ...
    conn.EmitEvent(edictID, "commit_pushed", payload) // Outbox pattern
    
    return true, tx.Commit()
}
```

### **Two-Phase Commit: Git + Database**

```go
func (f *Forge) forgeLing(ctx context.Context, tx *sql.Tx, ling Ling) error {
    conn := f.Conn.WithTx(tx)
    
    stagingID, err := conn.StageManifest(ling.EdictID, ling.LingID, ...)
    if err != nil {
        return err
    }
    
    commitHash, err := f.Git.Commit(patch, stagingID)
    if err != nil {
        conn.DeleteStagedManifest(stagingID)
        return fmt.Errorf("git commit failed: %w", err)
    }
    
    if err := conn.ActivateManifest(stagingID, commitHash); err != nil {
        f.Logger.Error("CRITICAL: Git commit orphaned", "commit_hash", commitHash)
        return err
    }
    
    return nil
}
```

### **Retry Policy**

```go
type RetryPolicy struct {
    MaxAttempts     int           // 3
    InitialBackoff  time.Duration // 1s
    MaxBackoff      time.Duration // 30s
    BackoffFactor   float64       // 2.0
    RetryableErrors []string      // "connection_refused", "timeout", "deadlock_detected"
}
```

**Non-Retryable Errors**: `zhengming_required`, `constraint_violation`, `invalid_state`, `authentication_failed`

### **Circuit Breaker**

```go
var CircuitBreakers = map[string]CircuitBreaker{
    "github": {FailureThreshold: 5, ResetTimeout: 60 * time.Second},
    "ci":     {FailureThreshold: 3, ResetTimeout: 30 * time.Second},
    "slack":  {FailureThreshold: 10, ResetTimeout: 120 * time.Second},
}
```

---

## **External Integrations**

### **GitHub**

```go
type GitHubConfig struct {
    AppID          int64
    InstallationID int64
    PrivateKeyPath string
    Token          string // Alternative: PAT
    WebhookSecret  string
    BaseURL        string // For GitHub Enterprise
}
```

**Webhook Events**: `issues.assigned`, `issue_comment.created`, `check_run.completed`

### **Git**

```go
type GitConfig struct {
    SSHKeyPath    string
    HTTPSToken    string
    DefaultBranch string // "main"
    RemoteName    string // "origin"
    AuthorName    string // "Asimi Forge"
    AuthorEmail   string // "forge@asimi.dev"
}
```

**Branching Strategy**: `asimi/edict-{id}` per edict, merged to `main` when complete.

### **CI (GitHub Actions)**

```yaml
name: Asimi CI
on:
  push:
    branches: ['asimi/**']
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: make test
```

### **Linter**

```go
type LinterConfig struct {
    Command               string   // "golangci-lint"
    Args                  []string // ["run", "--out-format=json"]
    ConfigFile            string   // ".golangci.yml"
    TreatWarningsAsErrors bool
    IgnoredRules          []string
}
```

### **Slack**

```go
type SlackConfig struct {
    BotToken       string // xoxb-...
    CouncilChannel string // "#asimi-council"
    IncidentChannel string // "#asimi-incidents"
}
```

### **Sentry**

```go
type SentryConfig struct {
    AuthToken     string
    Organization  string
    Project       string
    WebhookSecret string
}
```

---

## **Edge Cases: Detailed Handling**

### **1. Edict Cancellation**

```go
func (c *Chancellor) CancelEdict(edictID, reason, cancelledBy string) error {
    release, _ := c.Lock.Acquire(ctx, edictID)
    defer release()
    
    return c.DB.Transaction(func(tx *sql.Tx) error {
        conn := c.Conn.WithTx(tx)
        conn.UpdatePhase(edictID, "cancelled")
        conn.CancelPendingZhengming(edictID)
        c.Git.DeleteBranch(fmt.Sprintf("asimi/edict-%s", edictID))
        conn.EmitEvent(edictID, "edict_cancelled", EdictCancelledPayload{...})
        return c.GitHub.CreateIssueComment(edictID, fmt.Sprintf("🚫 **Edict Cancelled** by @%s\n\nReason: %s", cancelledBy, reason))
    })
}
```

### **2. Requirement Change**

| Current Phase | Action |
|---------------|--------|
| `planning` | Restart: delete ling, re-invoke Strategist |
| `forging` | Zhengming: "Continue with existing ling or re-plan?" |
| `judgment` | Complete current judgment, then assess |
| `review` | Complete review; if merged, open new edict |

### **3. Merge Conflicts**

```go
func (c *Chancellor) handleMergeConflict(edictID, branchName string) error {
    if err := c.Git.RebaseOnMain(branchName); err == nil {
        c.Conn.UpdatePhase(edictID, "judgment") // Re-run CI
        return nil
    }
    
    conflicts := c.Git.GetConflictFiles(branchName)
    question := fmt.Sprintf("Merge conflict in: %s\nOptions: /clarify resolve | /clarify manual | /clarify abort", strings.Join(conflicts, ", "))
    c.Conn.RequestZhengming(edictID, question, "urgent")
    return nil
}
```

### **4. Staleness Detection**

```go
func (rg *RitualGuard) CheckStaleEdicts() {
    staleEdicts := rg.Conn.GetStaleEdicts(7 * 24 * time.Hour)
    
    for _, edict := range staleEdicts {
        staleDays := int(time.Since(edict.LastActivityAt).Hours() / 24)
        
        if staleDays > 30 {
            rg.Chancellor.CancelEdict(edict.EdictID, "Auto-cancelled: inactivity", "system")
        } else if staleDays > 14 {
            rg.EscalateStaleEdict(edict, "critical")
        } else {
            rg.PostStaleWarning(edict)
        }
    }
}
```

### **5. Minister Crash Recovery**

```go
func (rg *RitualGuard) Run(eventStream <-chan TianEvent) {
    for event := range eventStream {
        release, _ := rg.Lock.Acquire(ctx, event.EdictID)
        
        func() {
            defer release()
            defer func() {
                if r := recover(); r != nil {
                    rg.Logger.Error("minister panic", "event_id", event.EventID, "panic", r)
                    // Don't acknowledge: event will be reprocessed
                }
            }()
            
            if err := rg.processEvent(event); err == nil {
                rg.EventBus.Acknowledge(event.EventID)
            }
        }()
    }
}
```

### **6. Circular Ling Dependencies**

```go
func (s *Strategist) validateDependencies(ling []Ling) error {
    graph := make(map[string][]string)
    for _, l := range ling {
        graph[l.LingID] = l.Dependencies
    }
    
    visited := make(map[string]int)
    var hasCycle func(string) bool
    hasCycle = func(id string) bool {
        if visited[id] == 1 { return true }
        if visited[id] == 2 { return false }
        visited[id] = 1
        for _, dep := range graph[id] {
            if hasCycle(dep) { return true }
        }
        visited[id] = 2
        return false
    }
    
    for id := range graph {
        if hasCycle(id) {
            return fmt.Errorf("circular dependency: %s", id)
        }
    }
    return nil
}
```

---

## Implementation Plan (Minimal)

## **Phase 0: Foundation** (Week 1-2)

- [ ] **P0.1**: Initialize Go module
- [ ] **P0.2**: Create repository structure
- [ ] **P0.3**: Set up development database
- [ ] **P0.4**: Define `config.Config` struct

---

## **Phase 1: Imperial Archives & Event Infrastructure** (Week 2-4)

- [ ] **P1.1**: Implement GORM models
- [ ] **P1.2**: Create idempotency key generator
- [ ] **P1.3**: Build database connection layer
- [ ] **P1.4**: Implement advisory lock system
- [ ] **P1.5**: Build event bus with PostgreSQL NOTIFY
- [ ] **P1.6**: Create transaction-aware event emitter

---

## **Phase 2: Minister Connections** (Week 4-6)

- [ ] **P2.1**: Implement `ChancellorConn` interface
- [ ] **P2.2**: Implement `StrategistConn` interface
- [ ] **P2.3**: Implement `ForgeConn` interface
- [ ] **P2.4**: Implement `JudgeConn` interface
- [ ] **P2.5**: Implement `CensorConn` interface
- [ ] **P2.6**: Implement `MarshalConn` interface
- [ ] **P2.7**: Create `RulerCouncil` seed data

---

## **Phase 3: Core Integrations** (Week 6-8)

- [ ] **P3.1**: Implement GitHub App client
- [ ] **P3.2**: Build Git operations wrapper
- [ ] **P3.3**: Implement CI pipeline trigger
- [ ] **P3.4**: Create linter integration
- [ ] **P3.5**: Implement Sentry webhook handler

---

## **Phase 4: Minister Core Logic** (Week 8-12)

- [ ] **P4.1**: Build Chancellor minister
- [ ] **P4.2**: Implement Strategist minister
- [ ] **P4.3**: Build Forge minister
- [ ] **P4.4**: Implement Judge minister
- [ ] **P4.5**: Build Censor minister
- [ ] **P4.6**: Implement Marshal minister
- [ ] **P4.7**: Create system prompt templates

---

## **Phase 5: Ritual Guard & Rituals** (Week 12-14)

- [ ] **P5.1**: Implement Ritual Guard event processor
- [ ] **P5.2**: Build flatline detection system
- [ ] **P5.3**: Implement Zhengming escalation cron
- [ ] **P5.4**: Create stale edict janitor
- [ ] **P5.5**: Build state machine enforcement

---

## **Phase 6: Safety & Reliability** (Week 14-16)

- [ ] **P6.1**: Implement retry policy engine
- [ ] **P6.2**: Build circuit breakers
- [ ] **P6.3**: Add comprehensive logging
- [ ] **P6.4**: Implement metrics collection
- [ ] **P6.5**: Create transaction safety tests

---

## **Phase 7: Deployment** (Week 16-18)

- [ ] **P7.1**: Containerize services
- [ ] **P7.2**: Write deployment manifests
- [ ] **P7.3**: Configure mTLS for query endpoint
- [ ] **P7.4**: Configure GitHub App
- [ ] **P7.5**: Set up monitoring dashboards

---

## **Phase 8: Testing** (Week 18-20)

- [ ] **P8.1**: Write unit tests for each minister
- [ ] **P8.2**: Create integration test suite
- [ ] **P8.3**: Perform chaos engineering tests
- [ ] **P8.4**: Execute load testing
- [ ] **P8.5**: Conduct security audit

---

## **Phase 9: Documentation** (Week 20)

- [ ] **P9.1**: Write API documentation
- [ ] **P9.2**: Create architecture decision records
- [ ] **P9.3**: Document $San Cai$ philosophy
- [ ] **P9.4**: Create council onboarding guide

---

## **Phase 10: Production Launch** (Week 21)

- [ ] **P10.1**: Deploy to staging
- [ ] **P10.2**: Execute gradual production rollout
- [ ] **P10.3**: Establish support rotation
- [ ] **P10.4**: Conduct post-launch retrospective
