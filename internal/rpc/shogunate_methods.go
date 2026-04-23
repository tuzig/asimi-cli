package rpc

// Method names for the Shogunate RPC surface. Kept in one place so the
// server (RegisterShogunateHandlers) and client (ShogunateClient) stay
// in sync. Method names double as msgpack dispatch keys.
const (
	MethodHasMinister          = "HasMinister"
	MethodResetMinisterSession = "ResetMinisterSession"
	MethodEdictKey             = "EdictKey"
	MethodCourtEdictKey        = "CourtEdictKey"
	MethodCreateEdict          = "CreateEdict"
	MethodCreateEdictSilent    = "CreateEdictSilent"
	MethodGetEdict             = "GetEdict"
	MethodPublishEvent         = "PublishEvent"
	MethodGrantRulerSeal       = "GrantRulerSeal"
	MethodGetEdictSeals        = "GetEdictSeals"
	MethodListActiveEdicts     = "ListActiveEdicts"
	MethodSubmitPrompt         = "SubmitPrompt"
	MethodRestoreMinisterSess  = "RestoreMinisterSession"
	MethodHandleZhengming      = "HandleZhengmingResponse"
	MethodCancelZhengming      = "CancelZhengming"
	MethodAllowRunnerFallback  = "AllowRunnerFallback"
	MethodRunShellCommand      = "RunShellCommand"
	MethodSessionState         = "SessionState"
	MethodAddSessionCtxFile    = "AddSessionContextFile"
	MethodAddSessionMessage    = "AddSessionMessage"
	MethodClearSessionHistory  = "ClearSessionHistory"
	MethodRollbackSession      = "RollbackSession"
	MethodCompactSession       = "CompactSession"
	MethodTakeSnapshot         = "TakeSnapshot"
	MethodCancelTab            = "CancelTab"
)
