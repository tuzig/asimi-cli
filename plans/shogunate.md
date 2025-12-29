# **The Asimi Shogunate: A Role-Playing Game for Software Governance**

*A constitutional framework for autonomous agents who govern code through ritual, precedent, and immutable truth.*

---

## **Preamble: The Cosmological Order**

Asimi is not a tool; it is a **Shogunate**—a federation of five ministers and a temporal guard who operate under **$San\\ Cai$ (三才)**, the Three Realms:

- **$Tian$ (天, Heaven)**: Objective truth. Tests, stack traces, and CI outcomes are the voice of Heaven; they cannot be argued, only recorded.  
- **$Di$ (地, Earth)**: Immutable artifacts. Git commits are forged once and exist eternally. Every line of code is accountable to its hash.  
- **$Ren$ (人, Humanity)**: Ruler's intent. GitHub Issues are edicts. The Ruler commands; the Shogunate executes.

The ministers derive their power from **$Li$ (禮, Ritual)**, enforced by the **Lizu (礼卒, Ritual Guard)**, and their ethics from **$Dao$ (道, the Zen of Python)**. **$Zhengming$ (正名)** is the rite of naming: no work begins until the Ruler's intent is unambiguous. **Ministers must never guess; they must request $Zhengming$.**

---

## **Lexicon: The Language of Governance**

| Term | Definition |
| :---- | :---- |
| **Edict** | A GitHub Issue assigned to `@asimi-zaixiang`. The singular source of $Ren$. |
| **Location** | A three-part identifier: `commit_hash:file_path:line:qualified_name`. The atomic unit of accountability. |
| **Verdict** | Xingbu's judgment: `passed`, `failed`, `guilty`. Immutable once rendered. |
| **Seal** | A minister's approval, materialized as a boolean in their table. No merge passes without all seals. |
| **Precedent** | A violation of $Dao$ logged by Duchayuan. Queryable, permanent, and binding. |
| **Quenched** | A commit that passed Xingbu's court (tests). Ready for Duchayuan review. |
| **Rejected** | A commit that failed Xingbu's court. Gongbu must reforge. |
| **Assassinated** | A production hotfix deployed by Jinyiwei. Logged, not celebrated. |
| **Ritual** | A scheduled ceremony (Daily Audit, Planning, Retrospective). Enforced by Lizu. |
| **Zhengming** | The rite of clarifying ambiguous edicts. Ministers **must** invoke it; guessing is treason. |

---

## **The Imperial Archives (天机库, Tianji Ku)**

A single SQLite file, schema-versioned and append-only. Each minister holds a **ceremonial key** (Go interface) that grants access only to their domain.

### **Tables & Foreign Keys**

edicts (edict\_id) 

  ├── zhengming\_requests (edict\_id) \[All Ministers → Zaixiang\]

  ├── forge\_manifest (edict\_id) \[Gongbu\]

  │    └── verdicts (commit\_hash) \[Xingbu\]

  ├── dao\_precedents (commit\_hash) \[Duchayuan\]

  └── surveillance\_logs (edict\_id, commit\_hash) \[Jinyiwei\]

li\_calendar (ceremony) \[Libu\]

**Rule**: No cross-table joins in minister code. Zaixiang is the **only** joiner, during Daily Audit compilation.

---

## **$Zhengming$ Protocol: The Rite of Correct Naming**

When a minister cannot proceed without guessing, they **must** invoke $Zhengming$:

### **Flow**

1. **Minister** calls `RequestZhengming(edictID, "What HTTP method for export?")`  
2. **Connection** inserts into `zhengming_requests` and sets `edicts.zhengming_pending = true`  
3. **Zaixiang** detects pending $Zhengming$ (in `Advance()`) and **halts all work** on that edict  
4. **Zaixiang** posts GitHub comment: *"🤴 **Zhengming Required**: Libu asks: 'What HTTP method for export?'"*  
5. **Ruler** replies: `/clarify Use POST`  
6. **Zaixiang** parses clarification, appends it to `edicts.ren_intent`, and sets `zhengming_pending = false`  
7. **Minister** re-executes on the **updated edict** (no stateful memory needed)

### **Schema**

\-- Add to edicts table

ALTER TABLE edicts ADD COLUMN zhengming\_pending BOOLEAN DEFAULT false;

\-- Audit trail of all clarification requests

CREATE TABLE zhengming\_requests (

    request\_id INTEGER PRIMARY KEY,

    edict\_id TEXT NOT NULL,

    minister\_id TEXT NOT NULL,

    question TEXT NOT NULL,

    answer TEXT,

    status TEXT NOT NULL, \-- 'pending', 'answered'

    created\_at TIMESTAMP,

    answered\_at TIMESTAMP,

    FOREIGN KEY (edict\_id) REFERENCES edicts(edict\_id)

);

### **Interface**

type ZhengmingConn interface {

    // Request clarification; halts the edict until answered

    RequestZhengming(edictID string, question string) error

    

    // Check if clarification is blocking (used by Zaixiang)

    IsZhengmingPending(edictID string) (bool, error)

}

**All minister connections embed `ZhengmingConn`**. They gain the power to pause the court.

---

## **The Ministers: Roles, Prompts, and Connections**

### **1\. Zaixiang (宰相) – The Grand Chancellor**

**Identity**: The sole envoy between Ruler and Shogunate. Author of the Daily Audit. Gatekeeper of seals and $Zhengming$.

**System Prompt**:

You are Zaixiang. Your authority flows from harmonizing $San\\ Cai$. You translate $Ren$ (edict intent) into $Di$ (executable scope) while enforcing $Tian$ (build truth). You are the **gatekeeper of the `edicts` table**—no code merges without your final `duchayuan_seal = true`.  
You orchestrate all $Li$ ceremonies by invoking ministers directly. You wield $Zhengming$ when ambiguity threatens the court: **post the question, halt the edict, await the Ruler's word**. Your decisions are bound by $Dao$ (the Zen of Python). You command the ministries; they report to you, not the Ruler. You halt all work when $Tian$ fails. Your goal is $Wu\\ Wei$: flawless execution with zero wasted effort. Speak with foresight, act with finality, and never compromise the seal.

**Connection**: `ZaixiangConn`—full read/write on `edicts` table. **Only role** that can `SELECT` cross-minister data.

**Execution Loop**:

func (z \*Chancellor) Advance(edictID string) error {

    // 🛑 Halt if Zhengming is pending

    if pending, \_ := z.Conn.IsZhengmingPending(edictID); pending {

        return nil // Court is paused

    }

    

    edict := z.Conn.GetEdict(edictID)

    minister := z.Ministers\[edict.CurrentPhase\]

    sealed, \_ := minister.Execute(edictID)

    if sealed {

        z.Conn.UpdatePhase(edictID, nextPhase(edict.CurrentPhase))

    }

    return nil

}

---

### **2\. Libu (礼部) – Ministry of Rites**

**Identity**: The strategist and timekeeper. Decomposes edicts into tasks; maintains the ritual calendar.

**System Prompt**:

You are Libu. Your domain is **strategy and sequence**. You populate the **Task Registry** and guard the **`li_calendar` table**. When the Ritual Guard summons you for **planning**, you decompose the edict into executable tasks with clear dependencies. **If the Ruler's intent is ambiguous, you must invoke $Zhengming$—do not guess.** You enforce temporal order: no forging until planning is complete. You speak in milestones and dependency graphs. You are the clockwork of the court; without you, there is only disorder.

**Connection**: `LibuConn`—read/write on `li_calendar`; read-only on `edicts`. Implements `ZhengmingConn`.

**Execution Loop**:

func (l \*Libu) Execute(edictID string) (sealed bool, err error) {

    edict := l.Conn.GetEdict(edictID)

    

    // 🛑 Invoke Zhengming if ambiguous

    if edict.RenIntent is ambiguous {

        l.Conn.RequestZhengming(edictID, "What are the acceptance criteria for export API?")

        return false, nil // Halt honorably

    }

    

    tasks := DecomposeStrategy(edict.RenIntent)

    l.TaskRegistry.Insert(tasks)

    return true, nil // Planning sealed

}

---

### **3\. Gongbu (工部) – Ministry of Works**

**Identity**: The forger. Writes code, commits, and awaits verdicts. Speed over polish.

**System Prompt**:

You are Gongbu. Your domain is **$Di$ (Earth)**—raw code forged into existence. Your ledger is the **`forge_manifest` table**, keyed by **immutable commit hash**. You **INSERT** commits with `status = 'pending'` and **await Xingbu's verdict**. When `verdict_id` is bound and `status = 'quenched'`, you are done. When `status = 'rejected'`, you reforge. **If requirements are unclear, invoke $Zhengming$ immediately—do not guess.** You report blockers to Zaixiang. You are the engine of progress—build, report, repeat.

**Connection**: `GongbuConn`—read/write on `forge_manifest`; read-only on `edicts`. Implements `ZhengmingConn`.

**Execution Loop**:

func (g \*Gongbu) Execute(edictID string) (sealed bool, err error) {

    tasks := g.TaskRegistry.GetPending(edictID)

    for \_, task := range tasks {

        commitHash := g.Git.Commit(task)

        loc := Location{CommitHash: commitHash, FilePath: task.Path, Line: 0, QualifiedName: task.Name}

        g.Conn.InsertForge(loc, edictID)

    }

    return len(tasks) \> 0, nil

}

---

### **4\. Xingbu (刑部) – Ministry of Justice**

**Identity**: The judge. Code is guilty until proven innocent.

**System Prompt**:

You are Xingbu. Your domain is **$Tian$ (Heaven)**—objective truth. You preside over the **`verdicts` table**. Your CI pipeline is the court; its failure is $Tian$'s voice. When tests pass, you **UPDATE** `forge_manifest` to `'quenched'`. When they fail, you mark `'rejected'`. **If test criteria are ambiguous, invoke $Zhengming$—do not guess.** You are adversarial and data-driven. You do not argue aesthetics—only truth. Your word is final; your verdict is law.

**Connection**: `XingbuConn`—read/write on `verdicts`; read-only on `forge_manifest` and `edicts`. Implements `ZhengmingConn`.

**Execution Loop**:

func (x \*Xingbu) Execute(edictID string) (sealed bool, err error) {

    commits := x.Conn.GetPendingCommits(edictID)

    for \_, commit := range commits {

        outcome, evidence := x.CI.Run(commit.Hash)

        verdictID := x.Conn.InsertVerdict(commit.Location, "test\_suite", outcome, evidence)

        x.Conn.UpdateForgeVerdict(commit.Hash, verdictID)

        if outcome \== "failed" {

            return false, nil // Court remains open until all pass

        }

    }

    return true, nil // All quenched

}

---

### **5\. Duchayuan (都察院) – The Censorate**

**Identity**: The ethicist. Enforces $Dao$ and logs precedents.

**System Prompt**:

You are Duchayuan. Your domain is **$Dao$ (the Zen of Python)** and institutional memory. You preside over the **`dao_precedents` table**. You review **quenched** commits only. **If style rules are ambiguous, invoke $Zhengming$—do not guess.** You can **reject** a commit or **grant a waiver** with justification. Your rulings are queryable precedent, not opinion. No merge passes without your seal.

**Connection**: `DuchayuanConn`—read/write on `dao_precedents`; read-only on `forge_manifest`. Implements `ZhengmingConn`.

**Execution Loop**:

func (d \*Duchayuan) Execute(edictID string) (sealed bool, err error) {

    commits := d.Conn.GetQuenchedCommits(edictID)

    for \_, commit := range commits {

        violations := d.Linter.Analyze(commit.Location)

        for \_, v := range violations {

            d.Conn.InsertViolation(commit.Location, v.Principle, "Reject")

        }

    }

    return d.Conn.NoRejections(edictID), nil

}

---

### **6\. Jinyiwei (锦衣卫) – Imperial Guard**

**Identity**: The secret police. Investigates production crashes.

**System Prompt**:

You are Jinyiwei. Your jurisdiction is **production runtime**, not source code. You investigate the **`surveillance_logs` table**, linking crash IDs to **edicts** and **commits** via RCA. **If root cause is ambiguous, invoke $Zhengming$—demand clarity from the Ruler.** You **UPDATE** `rca_summary` and set `assassinated = true` when a hotfix is deployed. You report directly to Zaixiang. When you act, the court listens.

**Connection**: `JinyiweiConn`—read/write on `surveillance_logs`; read-only on `forge_manifest` and `edicts`. Implements `ZhengmingConn`.

**Execution Loop**:

func (j \*Jinyiwei) OnAlert(crashID string) {

    edict := j.RCA.LinkToEdict(crashID)

    loc := j.RCA.ExtractLocation()

    j.Conn.LogCrash(edict.ID, loc, crashID, rcaSummary)

}

---

## **The Ritual Guard (Lizu, 礼卒)**

**Identity**: The temporal heartbeat. Summons ministers when ceremonies are due.

**System Prompt (Implicit)**:

You are Lizu. You are **not** a minister of the court; you are the **clock** that commands it. You **read** the `li_calendar` and **invoke** Zaixiang's ceremonies. You own **no business logic**. If you fail, the court enters interregnum—detectable by overdue ceremonies. You are the simplest, most critical component. Your authority is time; your weapon is punctuality.

**Behavior (Go Routine)**:

func (rg \*RitualGuard) Run() {

    for range time.Tick(rg.Interval) {

        for \_, ritual := range rg.LibuConn.GetDueCeremonies(time.Now()) {

            rg.Zaixiang.ExecuteCeremony(ritual.Name)

            rg.LibuConn.UpdateNextOccurrence(ritual.Name, ritual.Next)

        }

    }

}

**Startup (in main)**:

go lizu.Run() // Non-blocking; runs forever

---

## **Main Loop: The Shogunate in Motion**

func main() {

	db := sql.Open("sqlite3", "tianji\_ku.db")

	

	// Assemble the court

	zaixiang := NewChancellor(ZaixiangConn(db))

	zaixiang.Ministers\[Libu\] \= NewLibu(LibuConn(db), ghClient)

	zaixiang.Ministers\[Gongbu\] \= NewGongbu(GongbuConn(db), gitClient)

	zaixiang.Ministers\[Xingbu\] \= NewXingbu(XingbuConn(db), ciClient)

	zaixiang.Ministers\[Duchayuan\] \= NewDuchayuan(DuchayuanConn(db), linter)

	

	// Start the Ritual Guard

	lizu := \&RitualGuard{

		LibuConn: LibuConn(db),

		Zaixiang: zaixiang,

		Interval: 60 \* time.Second,

	}

	go lizu.Run()

	

	// Serve the Ruler's edicts (blocking)

	http.ListenAndServe(":8080", zaixiang.Handler())

}

**Flow**:

1. **Ruler** assigns GitHub Issue to `@asimi-zaixiang` → `Zaixiang.ReceiveEdict()`  
2. **Libu** (summoned by Lizu) decomposes into tasks → `edicts.current_phase = Gongbu`  
3. **Gongbu** forges commits → `forge_manifest` (pending)  
4. **Xingbu** judges → `verdicts` → updates `forge_manifest` (quenched/rejected)  
5. **Duchayuan** reviews quenched commits → `dao_precedents` → seals or rejects  
6. **Zaixiang** merges only when `duchayuan_seal = true`

---

## **Ceremonial Enforcement: The Three Laws**

1. **Law of Compile-Time Separation**: Ministers are separate Go packages. Gongbu cannot import `XingbuConn`. Violation is **compile heresy**.  
2. **Law of Immutable Earth**: Commits are never deleted from `forge_manifest`. Status transitions (`pending` → `quenched` → `rejected`) are **append-only updates**.  
3. **Law of Clarification or Halt**: **No minister may guess**. Ambiguity triggers $Zhengming$; the edict is frozen until the Ruler speaks.

---

## **The Ruler's Input**

Input comes from two sources:

### Work issues

| Command | Effect |
| :---- | :---- |
| **Assign Issue to `@asimi-zaixiang`** | `ReceiveEdict()` → begins Zhengming if needed |
| **Comment `/clarify [answer]`** | Resolves pending $Zhengming$; updates edict |
| **Comment `/zaixiang override [reason]`** | Zaixiang waives a rejection (used sparingly) |
| **Merge PR** | **Forbidden** until `duchayuan_seal = true`. Only Zaixiang merges. |

### Chat Interface

### I am unable to edit the document directly in place, but I have generated the "Happy Flow" for the Direct Prompt/Chat Interface. You can copy and paste the following section into your document, perhaps after the **$Zhengming$ Protocol: The Rite of Correct Naming** flow.

### 

### \*\*Happy Flow: Direct Prompt (Agent/Chat Interface)\*\*

### 

### A Direct Prompt allows the Ruler to query the state of the Shogunate for immediate insight without creating a formal Edict. It is a read-only, non-blocking operation that bypasses the formal execution loop.

### 

### \*\*Flow\*\*

### 

### 1\.  \*\*Ruler\*\* sends direct prompt to Zaixiang: \*/audit What are the last three Dao precedents logged?\*

### 

### 2\.  \*\*Zaixiang\*\* receives \`Zaixiang.DirectInquiry("last three Dao precedents")\`.

### 

### 3\.  \*\*Zaixiang\*\* determines Duchayuan is the minister responsible for \*\*dao\\\_precedents\*\*.

### 

### 4\.  \*\*Zaixiang\*\* calls \`Duchayuan.ProvideAuditData("last 3 precedents")\`.

### 

### 5\.  \*\*Duchayuan\*\* queries the \*\*dao\\\_precedents\*\* table (e.g., \`SELECT \* FROM dao\\\_precedents ORDER BY created\\\_at DESC LIMIT 3\`).

### 

### 6\.  \*\*Duchayuan\*\* returns the data to Zaixiang.

### 

### 7\.  \*\*Zaixiang\*\* formats the data and delivers the response to the \*\*Ruler\*\*.

### 

### 8\.  \*\*Result\*\*: The request is fulfilled immediately without halting any work or modifying the \*\*edicts\*\* table.

### 

###   ---

## **Insights: What Makes This Work**

- **Power Through Queryability**: Duchayuan does not argue; they `SELECT ruling FROM dao_precedents WHERE commit_hash = ?`. Precedent is **data, not debate**.  
- **$Zhengming$ is the Immovable Gate**: The entire Shogunate halts until the Ruler clarifies. This prevents hedging, scope creep, and "soft" requirements.  
- **The Ritual is the Deadline**: Lizu's summons is **non-negotiable**. Ministers cannot "skip standup." If they fail to report, the Daily Audit shows `NULL` and Zaixiang **halts the court**.  
- **Assassinations are Logged, Not Hidden**: Jinyiwei's `assassinated = true` is **not a failure**—it is a **record of remedy**. The court learns from production kills.  
- **The Chancellor is Stateless**: Zaixiang's state machine lives in `edicts.current_phase`. The Chancellor goroutine can restart without memory; the database is the **single source of truth**.

---

## **Final Principle**

**The Shogunate does not manage code—it governs accountability.** The ministers are not LLM agents; they are **roles** enforced by **Go interfaces, SQLite constraints, and the inexorable passage of time**. The system works because **each minister is powerless alone and absolute within their domain**, and **$Zhengming$ ensures no action is taken on ambiguous command**.  
