package tracing

const (
	attrRequestID     = "multica.request_id"
	attrRPCMethod     = "multica.rpc.method"
	attrRPCTransport  = "multica.rpc.transport"
	attrTaskID        = "multica.task.id"
	attrTaskAgentID   = "multica.task.agent_id"
	attrTaskIssueID   = "multica.task.issue_id"
	attrTaskRuntimeID = "multica.task.runtime_id"
	attrTaskAutopilot = "multica.task.autopilot_run_id"
	attrTaskEvidence  = "multica.task.trigger_evidence_kind"
	attrTriggerKind   = "multica.trigger.kind"
	attrTriggerRunID  = "multica.trigger.run_id"
	attrTriggerJob    = "multica.trigger.job"
	attrTriggerScope  = "multica.trigger.scope"

	spanTaskEnqueue   = "multica.task.enqueue"
	spanTaskClaim     = "multica.task.claim"
	spanTaskStart     = "multica.task.start"
	spanTaskComplete  = "multica.task.complete"
	spanTaskFail      = "multica.task.fail"
	spanTaskCancel    = "multica.task.cancel"
	spanTrigger       = "multica.trigger"
	spanRPCClaim      = "POST /api/daemon/tasks/claim"
	spanDaemonTask    = "multica.daemon.task"
	spanDaemonPrepare = "multica.daemon.prepare"
	spanDaemonLaunch  = "multica.daemon.launch"
	spanDaemonDrain   = "multica.daemon.drain"

	attrDaemonProvider  = "multica.daemon.provider"
	attrDaemonToolCount = "multica.daemon.tool_count"
)
