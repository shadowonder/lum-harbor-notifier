export namespace ailineage {
	
	export class Service {
	
	
	    static createFrom(source: any = {}) {
	        return new Service(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}

}

export namespace app {
	
	export class AICredentialStateDTO {
	    credentialConfigured: boolean;
	    restartRequired: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AICredentialStateDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.credentialConfigured = source["credentialConfigured"];
	        this.restartRequired = source["restartRequired"];
	    }
	}
	export class AIRuntimeStageDTO {
	    id: string;
	    name: string;
	    stage: string;
	    order: number;
	    actions: string[];
	    allActions: boolean;
	    enabled: boolean;
	    available: boolean;
	    extensionId?: string;
	    extensionName?: string;
	    instanceId?: string;
	    unavailableReason?: string;
	
	    static createFrom(source: any = {}) {
	        return new AIRuntimeStageDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.stage = source["stage"];
	        this.order = source["order"];
	        this.actions = source["actions"];
	        this.allActions = source["allActions"];
	        this.enabled = source["enabled"];
	        this.available = source["available"];
	        this.extensionId = source["extensionId"];
	        this.extensionName = source["extensionName"];
	        this.instanceId = source["instanceId"];
	        this.unavailableReason = source["unavailableReason"];
	    }
	}
	export class AIRuntimeDTO {
	    status: string;
	    issue?: string;
	    mode: string;
	    effectiveProviderId: string;
	    effectiveProviderName: string;
	    providerSource: string;
	    builtInConfigEditable: boolean;
	    managingExtensionId?: string;
	    managingExtensionName?: string;
	    managingProviderInstanceId?: string;
	    stages: AIRuntimeStageDTO[];
	
	    static createFrom(source: any = {}) {
	        return new AIRuntimeDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.issue = source["issue"];
	        this.mode = source["mode"];
	        this.effectiveProviderId = source["effectiveProviderId"];
	        this.effectiveProviderName = source["effectiveProviderName"];
	        this.providerSource = source["providerSource"];
	        this.builtInConfigEditable = source["builtInConfigEditable"];
	        this.managingExtensionId = source["managingExtensionId"];
	        this.managingExtensionName = source["managingExtensionName"];
	        this.managingProviderInstanceId = source["managingProviderInstanceId"];
	        this.stages = this.convertValues(source["stages"], AIRuntimeStageDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class AIRuntimeVerificationDTO {
	    success: boolean;
	    effectiveProviderId: string;
	    effectiveProviderName: string;
	    message: string;
	    elapsedMilliseconds: number;
	
	    static createFrom(source: any = {}) {
	        return new AIRuntimeVerificationDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.effectiveProviderId = source["effectiveProviderId"];
	        this.effectiveProviderName = source["effectiveProviderName"];
	        this.message = source["message"];
	        this.elapsedMilliseconds = source["elapsedMilliseconds"];
	    }
	}
	export class App {
	
	
	    static createFrom(source: any = {}) {
	        return new App(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class ApplyProposalRequestDTO {
	    id: string;
	
	    static createFrom(source: any = {}) {
	        return new ApplyProposalRequestDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	    }
	}
	export class WorkItemDTO {
	    id: string;
	    title: string;
	    description?: string;
	    lifecycleState: string;
	    completedAt?: string;
	    createdAt: string;
	    updatedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new WorkItemDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.description = source["description"];
	        this.lifecycleState = source["lifecycleState"];
	        this.completedAt = source["completedAt"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	    }
	}
	export class ProposalRelationDTO {
	    label: string;
	    value: string;
	    createdBySequenceNo?: number;
	
	    static createFrom(source: any = {}) {
	        return new ProposalRelationDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.value = source["value"];
	        this.createdBySequenceNo = source["createdBySequenceNo"];
	    }
	}
	export class ProposalOperationDTO {
	    id: string;
	    sequenceNo: number;
	    operationType: string;
	    label: string;
	    relations: ProposalRelationDTO[];
	    changes: ProposalChangeDTO[];
	
	    static createFrom(source: any = {}) {
	        return new ProposalOperationDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.sequenceNo = source["sequenceNo"];
	        this.operationType = source["operationType"];
	        this.label = source["label"];
	        this.relations = this.convertValues(source["relations"], ProposalRelationDTO);
	        this.changes = this.convertValues(source["changes"], ProposalChangeDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ProposalChangeDTO {
	    label: string;
	    before?: string;
	    after?: string;
	
	    static createFrom(source: any = {}) {
	        return new ProposalChangeDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.before = source["before"];
	        this.after = source["after"];
	    }
	}
	export class SourceProvenanceDTO {
	    extensionId: string;
	    extensionName: string;
	    externalId: string;
	    sourceKind: string;
	
	    static createFrom(source: any = {}) {
	        return new SourceProvenanceDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.extensionId = source["extensionId"];
	        this.extensionName = source["extensionName"];
	        this.externalId = source["externalId"];
	        this.sourceKind = source["sourceKind"];
	    }
	}
	export class ProposalDTO {
	    id: string;
	    targetWorkItemId?: string;
	    operationType: string;
	    title: string;
	    summary?: string;
	    reason?: string;
	    status: string;
	    baseTitle: string;
	    baseDescription?: string;
	    proposedTitle: string;
	    proposedDescription?: string;
	    createdAt: string;
	    updatedAt: string;
	    resolvedAt?: string;
	    aiRunId?: string;
	    sourceRevisionIds: string[];
	    sourceExtensionIds: string[];
	    sourceProvenances: SourceProvenanceDTO[];
	    changes: ProposalChangeDTO[];
	    operations: ProposalOperationDTO[];
	
	    static createFrom(source: any = {}) {
	        return new ProposalDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.targetWorkItemId = source["targetWorkItemId"];
	        this.operationType = source["operationType"];
	        this.title = source["title"];
	        this.summary = source["summary"];
	        this.reason = source["reason"];
	        this.status = source["status"];
	        this.baseTitle = source["baseTitle"];
	        this.baseDescription = source["baseDescription"];
	        this.proposedTitle = source["proposedTitle"];
	        this.proposedDescription = source["proposedDescription"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.resolvedAt = source["resolvedAt"];
	        this.aiRunId = source["aiRunId"];
	        this.sourceRevisionIds = source["sourceRevisionIds"];
	        this.sourceExtensionIds = source["sourceExtensionIds"];
	        this.sourceProvenances = this.convertValues(source["sourceProvenances"], SourceProvenanceDTO);
	        this.changes = this.convertValues(source["changes"], ProposalChangeDTO);
	        this.operations = this.convertValues(source["operations"], ProposalOperationDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ApplyProposalResultDTO {
	    outcome: string;
	    proposal: ProposalDTO;
	    workItem?: WorkItemDTO;
	
	    static createFrom(source: any = {}) {
	        return new ApplyProposalResultDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.outcome = source["outcome"];
	        this.proposal = this.convertValues(source["proposal"], ProposalDTO);
	        this.workItem = this.convertValues(source["workItem"], WorkItemDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class AskAssistantRequestDTO {
	    message: string;
	    surface: string;
	    workItemId: string;
	
	    static createFrom(source: any = {}) {
	        return new AskAssistantRequestDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.message = source["message"];
	        this.surface = source["surface"];
	        this.workItemId = source["workItemId"];
	    }
	}
	export class AssistantResultDTO {
	    kind: string;
	    text: string;
	    contextLabel: string;
	    changeApplied: boolean;
	    proposalOutcome: string;
	    proposalId: string;
	
	    static createFrom(source: any = {}) {
	        return new AssistantResultDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.text = source["text"];
	        this.contextLabel = source["contextLabel"];
	        this.changeApplied = source["changeApplied"];
	        this.proposalOutcome = source["proposalOutcome"];
	        this.proposalId = source["proposalId"];
	    }
	}
	export class BuiltInOpenAISettingsDTO {
	    endpoint: string;
	    model: string;
	    requestTimeoutSeconds: number;
	    useSystemCredentialManager: boolean;
	    credentialConfigured: boolean;
	    restartRequired: boolean;
	
	    static createFrom(source: any = {}) {
	        return new BuiltInOpenAISettingsDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.endpoint = source["endpoint"];
	        this.model = source["model"];
	        this.requestTimeoutSeconds = source["requestTimeoutSeconds"];
	        this.useSystemCredentialManager = source["useSystemCredentialManager"];
	        this.credentialConfigured = source["credentialConfigured"];
	        this.restartRequired = source["restartRequired"];
	    }
	}
	export class CalendarEventDTO {
	    id: string;
	    authority: string;
	    title: string;
	    details?: string;
	    location?: string;
	    startAt?: string;
	    endAt?: string;
	    startDate?: string;
	    endDate?: string;
	    isAllDay: boolean;
	    timezone?: string;
	
	    static createFrom(source: any = {}) {
	        return new CalendarEventDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.authority = source["authority"];
	        this.title = source["title"];
	        this.details = source["details"];
	        this.location = source["location"];
	        this.startAt = source["startAt"];
	        this.endAt = source["endAt"];
	        this.startDate = source["startDate"];
	        this.endDate = source["endDate"];
	        this.isAllDay = source["isAllDay"];
	        this.timezone = source["timezone"];
	    }
	}
	export class ConfiguredPluginInstanceDTO {
	    id: string;
	    extensionId: string;
	    capabilityId: string;
	    enabled: boolean;
	    config?: Record<string, any>;
	    nativeNotificationAuthorization?: string;
	
	    static createFrom(source: any = {}) {
	        return new ConfiguredPluginInstanceDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.extensionId = source["extensionId"];
	        this.capabilityId = source["capabilityId"];
	        this.enabled = source["enabled"];
	        this.config = source["config"];
	        this.nativeNotificationAuthorization = source["nativeNotificationAuthorization"];
	    }
	}
	export class CreateLocalTodoRequestDTO {
	    title: string;
	    details?: string;
	    dueDate?: string;
	    dueAt?: string;
	    workItemId?: string;
	    workflowId?: string;
	    workflowStepId?: string;
	    trackedItemId?: string;
	    offsetsSeconds?: number[];
	
	    static createFrom(source: any = {}) {
	        return new CreateLocalTodoRequestDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.title = source["title"];
	        this.details = source["details"];
	        this.dueDate = source["dueDate"];
	        this.dueAt = source["dueAt"];
	        this.workItemId = source["workItemId"];
	        this.workflowId = source["workflowId"];
	        this.workflowStepId = source["workflowStepId"];
	        this.trackedItemId = source["trackedItemId"];
	        this.offsetsSeconds = source["offsetsSeconds"];
	    }
	}
	export class CreateMonitorPointRequestDTO {
	    workItemId: string;
	    subjectTrackedItemId?: string;
	    kind: string;
	    title: string;
	    dueAt: string;
	    timezone: string;
	    expectedWorkItemLifecycle?: string;
	    expectedStepId?: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateMonitorPointRequestDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.workItemId = source["workItemId"];
	        this.subjectTrackedItemId = source["subjectTrackedItemId"];
	        this.kind = source["kind"];
	        this.title = source["title"];
	        this.dueAt = source["dueAt"];
	        this.timezone = source["timezone"];
	        this.expectedWorkItemLifecycle = source["expectedWorkItemLifecycle"];
	        this.expectedStepId = source["expectedStepId"];
	    }
	}
	export class CreatePluginInstanceRequestDTO {
	    extensionId: string;
	    capabilityId: string;
	    config?: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new CreatePluginInstanceRequestDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.extensionId = source["extensionId"];
	        this.capabilityId = source["capabilityId"];
	        this.config = source["config"];
	    }
	}
	export class CreateTrackedItemRequestDTO {
	    workItemId: string;
	    workflowId?: string;
	    parentItemId?: string;
	    title: string;
	    itemType?: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateTrackedItemRequestDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.workItemId = source["workItemId"];
	        this.workflowId = source["workflowId"];
	        this.parentItemId = source["parentItemId"];
	        this.title = source["title"];
	        this.itemType = source["itemType"];
	    }
	}
	export class CreateWorkItemRequestDTO {
	    title: string;
	    description?: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateWorkItemRequestDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.title = source["title"];
	        this.description = source["description"];
	    }
	}
	export class WorkflowStepRequestDTO {
	    key: string;
	    title: string;
	    terminal: boolean;
	
	    static createFrom(source: any = {}) {
	        return new WorkflowStepRequestDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.title = source["title"];
	        this.terminal = source["terminal"];
	    }
	}
	export class CreateWorkflowDefinitionRequestDTO {
	    name: string;
	    version: number;
	    steps: WorkflowStepRequestDTO[];
	
	    static createFrom(source: any = {}) {
	        return new CreateWorkflowDefinitionRequestDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.version = source["version"];
	        this.steps = this.convertValues(source["steps"], WorkflowStepRequestDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CreateWorkflowRequestDTO {
	    workItemId: string;
	    workflowDefinitionId: string;
	    currentStepId?: string;
	    startedAt?: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateWorkflowRequestDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.workItemId = source["workItemId"];
	        this.workflowDefinitionId = source["workflowDefinitionId"];
	        this.currentStepId = source["currentStepId"];
	        this.startedAt = source["startedAt"];
	    }
	}
	export class MonitorPointDTO {
	    id: string;
	    subjectKind: string;
	    subjectId: string;
	    kind: string;
	    title: string;
	    dueAt: string;
	    timezone: string;
	    expectationKind: string;
	    expectedWorkItemLifecycle?: string;
	    expectedStepId?: string;
	    state: string;
	    satisfiedAt?: string;
	    createdAt: string;
	    updatedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new MonitorPointDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.subjectKind = source["subjectKind"];
	        this.subjectId = source["subjectId"];
	        this.kind = source["kind"];
	        this.title = source["title"];
	        this.dueAt = source["dueAt"];
	        this.timezone = source["timezone"];
	        this.expectationKind = source["expectationKind"];
	        this.expectedWorkItemLifecycle = source["expectedWorkItemLifecycle"];
	        this.expectedStepId = source["expectedStepId"];
	        this.state = source["state"];
	        this.satisfiedAt = source["satisfiedAt"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	    }
	}
	export class ProgressionDTO {
	    totalSteps: number;
	    completedCount: number;
	    skippedCount: number;
	    passedCount: number;
	    progressPercent: number;
	    complete: boolean;
	    currentStepId?: string;
	    currentStepKey?: string;
	    currentState?: string;
	
	    static createFrom(source: any = {}) {
	        return new ProgressionDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.totalSteps = source["totalSteps"];
	        this.completedCount = source["completedCount"];
	        this.skippedCount = source["skippedCount"];
	        this.passedCount = source["passedCount"];
	        this.progressPercent = source["progressPercent"];
	        this.complete = source["complete"];
	        this.currentStepId = source["currentStepId"];
	        this.currentStepKey = source["currentStepKey"];
	        this.currentState = source["currentState"];
	    }
	}
	export class TrackedItemProgressionSummaryDTO {
	    trackedItemId: string;
	    parentItemId?: string;
	    workflowDefinitionId: string;
	    progression: ProgressionDTO;
	
	    static createFrom(source: any = {}) {
	        return new TrackedItemProgressionSummaryDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.trackedItemId = source["trackedItemId"];
	        this.parentItemId = source["parentItemId"];
	        this.workflowDefinitionId = source["workflowDefinitionId"];
	        this.progression = this.convertValues(source["progression"], ProgressionDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class WorkItemTrackingSummaryDTO {
	    workItemId: string;
	    trackedItemCount: number;
	    workflowBoundItemCount: number;
	    progressions: TrackedItemProgressionSummaryDTO[];
	    activeFinalTarget?: MonitorPointDTO;
	
	    static createFrom(source: any = {}) {
	        return new WorkItemTrackingSummaryDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.workItemId = source["workItemId"];
	        this.trackedItemCount = source["trackedItemCount"];
	        this.workflowBoundItemCount = source["workflowBoundItemCount"];
	        this.progressions = this.convertValues(source["progressions"], TrackedItemProgressionSummaryDTO);
	        this.activeFinalTarget = this.convertValues(source["activeFinalTarget"], MonitorPointDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DashboardCurrentWorkItemDTO {
	    id: string;
	    title: string;
	    description?: string;
	    lifecycleState: string;
	    updatedAt: string;
	    tracking: WorkItemTrackingSummaryDTO;
	    workflowStartedAt?: string;
	
	    static createFrom(source: any = {}) {
	        return new DashboardCurrentWorkItemDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.description = source["description"];
	        this.lifecycleState = source["lifecycleState"];
	        this.updatedAt = source["updatedAt"];
	        this.tracking = this.convertValues(source["tracking"], WorkItemTrackingSummaryDTO);
	        this.workflowStartedAt = source["workflowStartedAt"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DashboardProposalSummaryDTO {
	    pendingCount: number;
	    affectedWorkItemCount: number;
	
	    static createFrom(source: any = {}) {
	        return new DashboardProposalSummaryDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pendingCount = source["pendingCount"];
	        this.affectedWorkItemCount = source["affectedWorkItemCount"];
	    }
	}
	export class DashboardWorkItemSummaryDTO {
	    currentCount: number;
	    pausedCount: number;
	    completedCount: number;
	    cancelledCount: number;
	    closedCount: number;
	    currentItems: DashboardCurrentWorkItemDTO[];
	    allCurrentItems: DashboardCurrentWorkItemDTO[];
	    trackingAvailable: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DashboardWorkItemSummaryDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.currentCount = source["currentCount"];
	        this.pausedCount = source["pausedCount"];
	        this.completedCount = source["completedCount"];
	        this.cancelledCount = source["cancelledCount"];
	        this.closedCount = source["closedCount"];
	        this.currentItems = this.convertValues(source["currentItems"], DashboardCurrentWorkItemDTO);
	        this.allCurrentItems = this.convertValues(source["allCurrentItems"], DashboardCurrentWorkItemDTO);
	        this.trackingAvailable = source["trackingAvailable"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class PlannerExternalOriginDTO {
	    systemLabel: string;
	    providerName?: string;
	    extensionId?: string;
	    instanceId?: string;
	
	    static createFrom(source: any = {}) {
	        return new PlannerExternalOriginDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.systemLabel = source["systemLabel"];
	        this.providerName = source["providerName"];
	        this.extensionId = source["extensionId"];
	        this.instanceId = source["instanceId"];
	    }
	}
	export class PlannerRecentChangeDTO {
	    eventId: string;
	    eventType: string;
	    summary: string;
	    occurredAt: string;
	    actor: string;
	
	    static createFrom(source: any = {}) {
	        return new PlannerRecentChangeDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.eventId = source["eventId"];
	        this.eventType = source["eventType"];
	        this.summary = source["summary"];
	        this.occurredAt = source["occurredAt"];
	        this.actor = source["actor"];
	    }
	}
	export class TodoReminderDTO {
	    id: string;
	    plannerItemId: string;
	    state: string;
	    offsetsSeconds: number[];
	    firedOffsets: number[];
	    remindMode: string;
	
	    static createFrom(source: any = {}) {
	        return new TodoReminderDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.plannerItemId = source["plannerItemId"];
	        this.state = source["state"];
	        this.offsetsSeconds = source["offsetsSeconds"];
	        this.firedOffsets = source["firedOffsets"];
	        this.remindMode = source["remindMode"];
	    }
	}
	export class PlannerItemDTO {
	    id: string;
	    type: string;
	    title: string;
	    details?: string;
	    status: string;
	    authority: string;
	    scheduledAt?: string;
	    dueAt?: string;
	    scheduledDate?: string;
	    dueDate?: string;
	    completedAt?: string;
	    contextStatus: string;
	    missingContext: string[];
	    workItemId?: string;
	    workflowId?: string;
	    workflowStepId?: string;
	    workItemTitle?: string;
	    workflowName?: string;
	    workflowStepTitle?: string;
	    trackedItemId?: string;
	    trackedItemTitle?: string;
	    reminder?: TodoReminderDTO;
	    recentChanges?: PlannerRecentChangeDTO[];
	    localNote?: string;
	    externalOrigin?: PlannerExternalOriginDTO;
	
	    static createFrom(source: any = {}) {
	        return new PlannerItemDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.type = source["type"];
	        this.title = source["title"];
	        this.details = source["details"];
	        this.status = source["status"];
	        this.authority = source["authority"];
	        this.scheduledAt = source["scheduledAt"];
	        this.dueAt = source["dueAt"];
	        this.scheduledDate = source["scheduledDate"];
	        this.dueDate = source["dueDate"];
	        this.completedAt = source["completedAt"];
	        this.contextStatus = source["contextStatus"];
	        this.missingContext = source["missingContext"];
	        this.workItemId = source["workItemId"];
	        this.workflowId = source["workflowId"];
	        this.workflowStepId = source["workflowStepId"];
	        this.workItemTitle = source["workItemTitle"];
	        this.workflowName = source["workflowName"];
	        this.workflowStepTitle = source["workflowStepTitle"];
	        this.trackedItemId = source["trackedItemId"];
	        this.trackedItemTitle = source["trackedItemTitle"];
	        this.reminder = this.convertValues(source["reminder"], TodoReminderDTO);
	        this.recentChanges = this.convertValues(source["recentChanges"], PlannerRecentChangeDTO);
	        this.localNote = source["localNote"];
	        this.externalOrigin = this.convertValues(source["externalOrigin"], PlannerExternalOriginDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class PlannerViewDTO {
	    calendarEvents: CalendarEventDTO[];
	    calendarItems: PlannerItemDTO[];
	    currentOpenItems: PlannerItemDTO[];
	    completedItems: PlannerItemDTO[];
	    completedTodayItems: PlannerItemDTO[];
	
	    static createFrom(source: any = {}) {
	        return new PlannerViewDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.calendarEvents = this.convertValues(source["calendarEvents"], CalendarEventDTO);
	        this.calendarItems = this.convertValues(source["calendarItems"], PlannerItemDTO);
	        this.currentOpenItems = this.convertValues(source["currentOpenItems"], PlannerItemDTO);
	        this.completedItems = this.convertValues(source["completedItems"], PlannerItemDTO);
	        this.completedTodayItems = this.convertValues(source["completedTodayItems"], PlannerItemDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DashboardViewDTO {
	    planner: PlannerViewDTO;
	    workItems: DashboardWorkItemSummaryDTO;
	
	    static createFrom(source: any = {}) {
	        return new DashboardViewDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.planner = this.convertValues(source["planner"], PlannerViewDTO);
	        this.workItems = this.convertValues(source["workItems"], DashboardWorkItemSummaryDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DashboardViewRequestDTO {
	    rangeStart: string;
	    rangeEnd: string;
	    today: string;
	    timezone: string;
	
	    static createFrom(source: any = {}) {
	        return new DashboardViewRequestDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rangeStart = source["rangeStart"];
	        this.rangeEnd = source["rangeEnd"];
	        this.today = source["today"];
	        this.timezone = source["timezone"];
	    }
	}
	
	export class HistoryCausalSourceDTO {
	    sourceRevisionId: string;
	    extensionId: string;
	    instanceId: string;
	    sourceKind: string;
	    payloadJson: string;
	
	    static createFrom(source: any = {}) {
	        return new HistoryCausalSourceDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sourceRevisionId = source["sourceRevisionId"];
	        this.extensionId = source["extensionId"];
	        this.instanceId = source["instanceId"];
	        this.sourceKind = source["sourceKind"];
	        this.payloadJson = source["payloadJson"];
	    }
	}
	export class HistoryEventDTO {
	    id: string;
	    eventType: string;
	    scope: string;
	    actor: string;
	    summary: string;
	    occurredAt: string;
	    recordedAt: string;
	    correlationId: string;
	    causationEventId?: string;
	    subjectEntityId?: string;
	    workItemId?: string;
	    revisionId?: string;
	    proposalId?: string;
	    aiRunId?: string;
	    sourceRevisionId?: string;
	    before: Record<string, any>;
	    after: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new HistoryEventDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.eventType = source["eventType"];
	        this.scope = source["scope"];
	        this.actor = source["actor"];
	        this.summary = source["summary"];
	        this.occurredAt = source["occurredAt"];
	        this.recordedAt = source["recordedAt"];
	        this.correlationId = source["correlationId"];
	        this.causationEventId = source["causationEventId"];
	        this.subjectEntityId = source["subjectEntityId"];
	        this.workItemId = source["workItemId"];
	        this.revisionId = source["revisionId"];
	        this.proposalId = source["proposalId"];
	        this.aiRunId = source["aiRunId"];
	        this.sourceRevisionId = source["sourceRevisionId"];
	        this.before = source["before"];
	        this.after = source["after"];
	    }
	}
	export class HistoryCausalChainDTO {
	    targetEventId: string;
	    targetEvent?: HistoryEventDTO;
	    rootEvent?: HistoryEventDTO;
	    events: HistoryEventDTO[];
	    rootSource?: HistoryCausalSourceDTO;
	
	    static createFrom(source: any = {}) {
	        return new HistoryCausalChainDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.targetEventId = source["targetEventId"];
	        this.targetEvent = this.convertValues(source["targetEvent"], HistoryEventDTO);
	        this.rootEvent = this.convertValues(source["rootEvent"], HistoryEventDTO);
	        this.events = this.convertValues(source["events"], HistoryEventDTO);
	        this.rootSource = this.convertValues(source["rootSource"], HistoryCausalSourceDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	export class HistoryEventPageDTO {
	    asOf: string;
	    events: HistoryEventDTO[];
	    truncated: boolean;
	
	    static createFrom(source: any = {}) {
	        return new HistoryEventPageDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.asOf = source["asOf"];
	        this.events = this.convertValues(source["events"], HistoryEventDTO);
	        this.truncated = source["truncated"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class HistoryQueryRequestDTO {
	    days: number;
	    scope: string;
	    search: string;
	
	    static createFrom(source: any = {}) {
	        return new HistoryQueryRequestDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.days = source["days"];
	        this.scope = source["scope"];
	        this.search = source["search"];
	    }
	}
	export class HistoryTraceDTO {
	    correlationId: string;
	    events: HistoryEventDTO[];
	    truncated: boolean;
	
	    static createFrom(source: any = {}) {
	        return new HistoryTraceDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.correlationId = source["correlationId"];
	        this.events = this.convertValues(source["events"], HistoryEventDTO);
	        this.truncated = source["truncated"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class HistoryTraceSummaryDTO {
	    correlationId: string;
	    firstOccurredAt: string;
	    lastOccurredAt: string;
	    representativeSummary: string;
	    representativeScope: string;
	    eventCount: number;
	    workItemId?: string;
	    scopes: string[];
	
	    static createFrom(source: any = {}) {
	        return new HistoryTraceSummaryDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.correlationId = source["correlationId"];
	        this.firstOccurredAt = source["firstOccurredAt"];
	        this.lastOccurredAt = source["lastOccurredAt"];
	        this.representativeSummary = source["representativeSummary"];
	        this.representativeScope = source["representativeScope"];
	        this.eventCount = source["eventCount"];
	        this.workItemId = source["workItemId"];
	        this.scopes = source["scopes"];
	    }
	}
	export class HistoryTraceListDTO {
	    traces: HistoryTraceSummaryDTO[];
	    truncated: boolean;
	
	    static createFrom(source: any = {}) {
	        return new HistoryTraceListDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.traces = this.convertValues(source["traces"], HistoryTraceSummaryDTO);
	        this.truncated = source["truncated"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class InAppNotificationSettingsDTO {
	    autoDismiss: boolean;
	    autoDismissSeconds: number;
	    placement: string;
	
	    static createFrom(source: any = {}) {
	        return new InAppNotificationSettingsDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.autoDismiss = source["autoDismiss"];
	        this.autoDismissSeconds = source["autoDismissSeconds"];
	        this.placement = source["placement"];
	    }
	}
	export class InterpretSourceRequestDTO {
	    sourceRevisionId: string;
	
	    static createFrom(source: any = {}) {
	        return new InterpretSourceRequestDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sourceRevisionId = source["sourceRevisionId"];
	    }
	}
	export class LocalSourceCaptureRequestDTO {
	    title: string;
	    content: string;
	    sourceKind?: string;
	    externalId?: string;
	
	    static createFrom(source: any = {}) {
	        return new LocalSourceCaptureRequestDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.title = source["title"];
	        this.content = source["content"];
	        this.sourceKind = source["sourceKind"];
	        this.externalId = source["externalId"];
	    }
	}
	export class LocalSourceDTO {
	    sourceRecordId: string;
	    sourceRevisionId: string;
	    sourceKind: string;
	    title: string;
	    content: string;
	    contentHash: string;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new LocalSourceDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sourceRecordId = source["sourceRecordId"];
	        this.sourceRevisionId = source["sourceRevisionId"];
	        this.sourceKind = source["sourceKind"];
	        this.title = source["title"];
	        this.content = source["content"];
	        this.contentHash = source["contentHash"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class MarkNotificationReadRequestDTO {
	    notificationId: string;
	
	    static createFrom(source: any = {}) {
	        return new MarkNotificationReadRequestDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.notificationId = source["notificationId"];
	    }
	}
	
	export class NotificationActivationDTO {
	    notificationId: string;
	    plannerItemId?: string;
	    workItemId?: string;
	
	    static createFrom(source: any = {}) {
	        return new NotificationActivationDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.notificationId = source["notificationId"];
	        this.plannerItemId = source["plannerItemId"];
	        this.workItemId = source["workItemId"];
	    }
	}
	export class NotificationReasonDTO {
	    version: number;
	    monitorPointId: string;
	    monitorKind: string;
	    monitorTitle: string;
	    dueAt: string;
	    timezone: string;
	    subjectKind: string;
	    subjectId: string;
	    subjectTitle?: string;
	    expectedKind: string;
	    expectedWorkItemLifecycle?: string;
	    expectedStepId?: string;
	    expectedStepTitle?: string;
	    actualWorkItemLifecycle?: string;
	    actualStepState?: string;
	
	    static createFrom(source: any = {}) {
	        return new NotificationReasonDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.monitorPointId = source["monitorPointId"];
	        this.monitorKind = source["monitorKind"];
	        this.monitorTitle = source["monitorTitle"];
	        this.dueAt = source["dueAt"];
	        this.timezone = source["timezone"];
	        this.subjectKind = source["subjectKind"];
	        this.subjectId = source["subjectId"];
	        this.subjectTitle = source["subjectTitle"];
	        this.expectedKind = source["expectedKind"];
	        this.expectedWorkItemLifecycle = source["expectedWorkItemLifecycle"];
	        this.expectedStepId = source["expectedStepId"];
	        this.expectedStepTitle = source["expectedStepTitle"];
	        this.actualWorkItemLifecycle = source["actualWorkItemLifecycle"];
	        this.actualStepState = source["actualStepState"];
	    }
	}
	export class NotificationCenterItemDTO {
	    notificationId: string;
	    attentionIssueId: string;
	    workItemId?: string;
	    workItemTitle?: string;
	    subjectTrackedItemId?: string;
	    plannerItemId?: string;
	    title: string;
	    message: string;
	    severity: string;
	    issueType: string;
	    recommendedAction?: string;
	    deliveryState: string;
	    sentAt?: string;
	    readAt?: string;
	    dismissedAt?: string;
	    attentionState: string;
	    snoozedUntil?: string;
	    resolvedAt?: string;
	    disposition: string;
	    createdAt: string;
	    updatedAt: string;
	    reason: NotificationReasonDTO;
	
	    static createFrom(source: any = {}) {
	        return new NotificationCenterItemDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.notificationId = source["notificationId"];
	        this.attentionIssueId = source["attentionIssueId"];
	        this.workItemId = source["workItemId"];
	        this.workItemTitle = source["workItemTitle"];
	        this.subjectTrackedItemId = source["subjectTrackedItemId"];
	        this.plannerItemId = source["plannerItemId"];
	        this.title = source["title"];
	        this.message = source["message"];
	        this.severity = source["severity"];
	        this.issueType = source["issueType"];
	        this.recommendedAction = source["recommendedAction"];
	        this.deliveryState = source["deliveryState"];
	        this.sentAt = source["sentAt"];
	        this.readAt = source["readAt"];
	        this.dismissedAt = source["dismissedAt"];
	        this.attentionState = source["attentionState"];
	        this.snoozedUntil = source["snoozedUntil"];
	        this.resolvedAt = source["resolvedAt"];
	        this.disposition = source["disposition"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.reason = this.convertValues(source["reason"], NotificationReasonDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class NotificationCenterDTO {
	    at: string;
	    items: NotificationCenterItemDTO[];
	
	    static createFrom(source: any = {}) {
	        return new NotificationCenterDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.at = source["at"];
	        this.items = this.convertValues(source["items"], NotificationCenterItemDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class NotificationReadResultDTO {
	    notificationId: string;
	    readAt: string;
	
	    static createFrom(source: any = {}) {
	        return new NotificationReadResultDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.notificationId = source["notificationId"];
	        this.readAt = source["readAt"];
	    }
	}
	
	
	
	
	
	export class PlannerViewRequestDTO {
	    rangeStart: string;
	    rangeEnd: string;
	    today: string;
	    timezone: string;
	
	    static createFrom(source: any = {}) {
	        return new PlannerViewRequestDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rangeStart = source["rangeStart"];
	        this.rangeEnd = source["rangeEnd"];
	        this.today = source["today"];
	        this.timezone = source["timezone"];
	    }
	}
	export class PluginBindingDTO {
	    id: string;
	    type: string;
	    capabilityId: string;
	    actions: string[];
	
	    static createFrom(source: any = {}) {
	        return new PluginBindingDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.type = source["type"];
	        this.capabilityId = source["capabilityId"];
	        this.actions = source["actions"];
	    }
	}
	export class PluginConfigOptionDTO {
	    label: string;
	    value: string;
	
	    static createFrom(source: any = {}) {
	        return new PluginConfigOptionDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.value = source["value"];
	    }
	}
	export class PluginConfigFieldDTO {
	    key: string;
	    label: string;
	    description?: string;
	    type: string;
	    required: boolean;
	    default?: any;
	    options?: PluginConfigOptionDTO[];
	    visibility?: string;
	
	    static createFrom(source: any = {}) {
	        return new PluginConfigFieldDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.label = source["label"];
	        this.description = source["description"];
	        this.type = source["type"];
	        this.required = source["required"];
	        this.default = source["default"];
	        this.options = this.convertValues(source["options"], PluginConfigOptionDTO);
	        this.visibility = source["visibility"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class PluginInstallResultDTO {
	    extensionId: string;
	    version: string;
	    restartRequired: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PluginInstallResultDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.extensionId = source["extensionId"];
	        this.version = source["version"];
	        this.restartRequired = source["restartRequired"];
	    }
	}
	export class PluginInstanceCreationResultDTO {
	    instanceId: string;
	    extensionId: string;
	    capabilityId: string;
	    restartRequired: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PluginInstanceCreationResultDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.instanceId = source["instanceId"];
	        this.extensionId = source["extensionId"];
	        this.capabilityId = source["capabilityId"];
	        this.restartRequired = source["restartRequired"];
	    }
	}
	export class PluginInstanceMutationResultDTO {
	    instanceId: string;
	    extensionId: string;
	    capabilityId: string;
	    enabled: boolean;
	    restartRequired: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PluginInstanceMutationResultDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.instanceId = source["instanceId"];
	        this.extensionId = source["extensionId"];
	        this.capabilityId = source["capabilityId"];
	        this.enabled = source["enabled"];
	        this.restartRequired = source["restartRequired"];
	    }
	}
	export class PluginInventoryItemDTO {
	    id: string;
	    name: string;
	    version: string;
	    protocolVersion: number;
	    installed: boolean;
	    selected: boolean;
	    running: boolean;
	    runtimeStatus: string;
	    capabilities: string[];
	    selectableCapabilities: string[];
	    bindings: PluginBindingDTO[];
	    instances: ConfiguredPluginInstanceDTO[];
	    configSchema: PluginConfigFieldDTO[];
	
	    static createFrom(source: any = {}) {
	        return new PluginInventoryItemDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.version = source["version"];
	        this.protocolVersion = source["protocolVersion"];
	        this.installed = source["installed"];
	        this.selected = source["selected"];
	        this.running = source["running"];
	        this.runtimeStatus = source["runtimeStatus"];
	        this.capabilities = source["capabilities"];
	        this.selectableCapabilities = source["selectableCapabilities"];
	        this.bindings = this.convertValues(source["bindings"], PluginBindingDTO);
	        this.instances = this.convertValues(source["instances"], ConfiguredPluginInstanceDTO);
	        this.configSchema = this.convertValues(source["configSchema"], PluginConfigFieldDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class PluginInventoryDTO {
	    plugins: PluginInventoryItemDTO[];
	
	    static createFrom(source: any = {}) {
	        return new PluginInventoryDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.plugins = this.convertValues(source["plugins"], PluginInventoryItemDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class PluginRemovalResultDTO {
	    extensionId: string;
	    restartRequired: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PluginRemovalResultDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.extensionId = source["extensionId"];
	        this.restartRequired = source["restartRequired"];
	    }
	}
	
	
	
	
	
	export class ProposeWorkItemUpdateRequestDTO {
	    targetWorkItemId: string;
	    proposedTitle: string;
	    proposedDescription?: string;
	    reason?: string;
	
	    static createFrom(source: any = {}) {
	        return new ProposeWorkItemUpdateRequestDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.targetWorkItemId = source["targetWorkItemId"];
	        this.proposedTitle = source["proposedTitle"];
	        this.proposedDescription = source["proposedDescription"];
	        this.reason = source["reason"];
	    }
	}
	export class RejectProposalRequestDTO {
	    id: string;
	
	    static createFrom(source: any = {}) {
	        return new RejectProposalRequestDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	    }
	}
	export class RuntimeNotificationsConfigDTO {
	    inApp: InAppNotificationSettingsDTO;
	
	    static createFrom(source: any = {}) {
	        return new RuntimeNotificationsConfigDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.inApp = this.convertValues(source["inApp"], InAppNotificationSettingsDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RuntimeDashboardConfigDTO {
	    todoDisplayLimit: number;
	
	    static createFrom(source: any = {}) {
	        return new RuntimeDashboardConfigDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.todoDisplayLimit = source["todoDisplayLimit"];
	    }
	}
	export class RuntimeGeneralConfigDTO {
	    closeWindowBehavior: string;
	
	    static createFrom(source: any = {}) {
	        return new RuntimeGeneralConfigDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.closeWindowBehavior = source["closeWindowBehavior"];
	    }
	}
	export class RuntimeConfigDTO {
	    general: RuntimeGeneralConfigDTO;
	    dashboard: RuntimeDashboardConfigDTO;
	    notifications: RuntimeNotificationsConfigDTO;
	    platform: string;
	
	    static createFrom(source: any = {}) {
	        return new RuntimeConfigDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.general = this.convertValues(source["general"], RuntimeGeneralConfigDTO);
	        this.dashboard = this.convertValues(source["dashboard"], RuntimeDashboardConfigDTO);
	        this.notifications = this.convertValues(source["notifications"], RuntimeNotificationsConfigDTO);
	        this.platform = source["platform"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	
	export class SaveBuiltInOpenAISettingsRequestDTO {
	    endpoint: string;
	    model: string;
	    requestTimeoutSeconds: number;
	    useSystemCredentialManager: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SaveBuiltInOpenAISettingsRequestDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.endpoint = source["endpoint"];
	        this.model = source["model"];
	        this.requestTimeoutSeconds = source["requestTimeoutSeconds"];
	        this.useSystemCredentialManager = source["useSystemCredentialManager"];
	    }
	}
	export class SaveGeneralSettingsRequestDTO {
	    closeWindowBehavior: string;
	
	    static createFrom(source: any = {}) {
	        return new SaveGeneralSettingsRequestDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.closeWindowBehavior = source["closeWindowBehavior"];
	    }
	}
	export class SaveInAppNotificationSettingsRequestDTO {
	    autoDismiss: boolean;
	    autoDismissSeconds: number;
	    placement: string;
	
	    static createFrom(source: any = {}) {
	        return new SaveInAppNotificationSettingsRequestDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.autoDismiss = source["autoDismiss"];
	        this.autoDismissSeconds = source["autoDismissSeconds"];
	        this.placement = source["placement"];
	    }
	}
	export class SetBuiltInOpenAICredentialRequestDTO {
	    credential: string;
	
	    static createFrom(source: any = {}) {
	        return new SetBuiltInOpenAICredentialRequestDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.credential = source["credential"];
	    }
	}
	export class SetMonitorPointStateRequestDTO {
	    id: string;
	    targetState: string;
	
	    static createFrom(source: any = {}) {
	        return new SetMonitorPointStateRequestDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.targetState = source["targetState"];
	    }
	}
	export class SetTodoReminderRequestDTO {
	    plannerItemId: string;
	    offsetsSeconds: number[];
	
	    static createFrom(source: any = {}) {
	        return new SetTodoReminderRequestDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.plannerItemId = source["plannerItemId"];
	        this.offsetsSeconds = source["offsetsSeconds"];
	    }
	}
	export class SetTrackedItemStepStateRequestDTO {
	    trackedItemId: string;
	    workflowStepId: string;
	    state: string;
	
	    static createFrom(source: any = {}) {
	        return new SetTrackedItemStepStateRequestDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.trackedItemId = source["trackedItemId"];
	        this.workflowStepId = source["workflowStepId"];
	        this.state = source["state"];
	    }
	}
	export class SetWorkItemLifecycleStateRequestDTO {
	    id: string;
	    targetLifecycleState: string;
	
	    static createFrom(source: any = {}) {
	        return new SetWorkItemLifecycleStateRequestDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.targetLifecycleState = source["targetLifecycleState"];
	    }
	}
	export class SetWorkflowCurrentStepRequestDTO {
	    workflowId: string;
	    stepId: string;
	
	    static createFrom(source: any = {}) {
	        return new SetWorkflowCurrentStepRequestDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.workflowId = source["workflowId"];
	        this.stepId = source["stepId"];
	    }
	}
	export class SnoozeAttentionIssueRequestDTO {
	    attentionIssueId: string;
	    snoozedUntil: string;
	
	    static createFrom(source: any = {}) {
	        return new SnoozeAttentionIssueRequestDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.attentionIssueId = source["attentionIssueId"];
	        this.snoozedUntil = source["snoozedUntil"];
	    }
	}
	export class SnoozeResultDTO {
	    attentionIssueId: string;
	    snoozedUntil: string;
	
	    static createFrom(source: any = {}) {
	        return new SnoozeResultDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.attentionIssueId = source["attentionIssueId"];
	        this.snoozedUntil = source["snoozedUntil"];
	    }
	}
	export class SourceInterpretationResultDTO {
	    sourceRevisionId: string;
	    cleanRunId: string;
	    interpretRunId: string;
	    summary: string;
	    hasProposal: boolean;
	    proposal?: ProposalDTO;
	    notice?: string;
	    alreadyPending?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SourceInterpretationResultDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sourceRevisionId = source["sourceRevisionId"];
	        this.cleanRunId = source["cleanRunId"];
	        this.interpretRunId = source["interpretRunId"];
	        this.summary = source["summary"];
	        this.hasProposal = source["hasProposal"];
	        this.proposal = this.convertValues(source["proposal"], ProposalDTO);
	        this.notice = source["notice"];
	        this.alreadyPending = source["alreadyPending"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class StepStateDTO {
	    workflowStepId: string;
	    state: string;
	    enteredAt?: string;
	    completedAt?: string;
	
	    static createFrom(source: any = {}) {
	        return new StepStateDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.workflowStepId = source["workflowStepId"];
	        this.state = source["state"];
	        this.enteredAt = source["enteredAt"];
	        this.completedAt = source["completedAt"];
	    }
	}
	
	export class TrackedItemAttentionDTO {
	    id: string;
	    subjectTrackedItemId: string;
	    monitorPointId: string;
	    state: string;
	    severity: string;
	    reason: string;
	    recommendedAction?: string;
	    snoozedUntil?: string;
	    resolvedAt?: string;
	
	    static createFrom(source: any = {}) {
	        return new TrackedItemAttentionDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.subjectTrackedItemId = source["subjectTrackedItemId"];
	        this.monitorPointId = source["monitorPointId"];
	        this.state = source["state"];
	        this.severity = source["severity"];
	        this.reason = source["reason"];
	        this.recommendedAction = source["recommendedAction"];
	        this.snoozedUntil = source["snoozedUntil"];
	        this.resolvedAt = source["resolvedAt"];
	    }
	}
	export class TrackedItemDTO {
	    id: string;
	    workItemId: string;
	    workflowId: string;
	    parentItemId?: string;
	    title: string;
	    itemType?: string;
	    lifecycleState: string;
	    completedAt?: string;
	    createdAt: string;
	    updatedAt: string;
	    stepStates: StepStateDTO[];
	    progression?: ProgressionDTO;
	
	    static createFrom(source: any = {}) {
	        return new TrackedItemDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.workItemId = source["workItemId"];
	        this.workflowId = source["workflowId"];
	        this.parentItemId = source["parentItemId"];
	        this.title = source["title"];
	        this.itemType = source["itemType"];
	        this.lifecycleState = source["lifecycleState"];
	        this.completedAt = source["completedAt"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.stepStates = this.convertValues(source["stepStates"], StepStateDTO);
	        this.progression = this.convertValues(source["progression"], ProgressionDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class TrackedItemRelationDTO {
	    fromTrackedItemId: string;
	    toTrackedItemId: string;
	    relationType: string;
	
	    static createFrom(source: any = {}) {
	        return new TrackedItemRelationDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fromTrackedItemId = source["fromTrackedItemId"];
	        this.toTrackedItemId = source["toTrackedItemId"];
	        this.relationType = source["relationType"];
	    }
	}
	export class UpdatePluginInstanceConfigRequestDTO {
	    instanceId: string;
	    config?: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new UpdatePluginInstanceConfigRequestDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.instanceId = source["instanceId"];
	        this.config = source["config"];
	    }
	}
	
	export class WorkItemOverviewAttentionDTO {
	    openIssueCount: number;
	    actionableIssueCount: number;
	    snoozedIssueCount: number;
	    highestActiveSeverity?: string;
	
	    static createFrom(source: any = {}) {
	        return new WorkItemOverviewAttentionDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.openIssueCount = source["openIssueCount"];
	        this.actionableIssueCount = source["actionableIssueCount"];
	        this.snoozedIssueCount = source["snoozedIssueCount"];
	        this.highestActiveSeverity = source["highestActiveSeverity"];
	    }
	}
	export class WorkItemOverviewStateDistributionDTO {
	    notStarted: number;
	    inProgress: number;
	    waiting: number;
	    blocked: number;
	    failed: number;
	    unknown: number;
	    skipped: number;
	    completed: number;
	
	    static createFrom(source: any = {}) {
	        return new WorkItemOverviewStateDistributionDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.notStarted = source["notStarted"];
	        this.inProgress = source["inProgress"];
	        this.waiting = source["waiting"];
	        this.blocked = source["blocked"];
	        this.failed = source["failed"];
	        this.unknown = source["unknown"];
	        this.skipped = source["skipped"];
	        this.completed = source["completed"];
	    }
	}
	export class WorkItemOverviewTrackedItemSignalDTO {
	    state: string;
	    hasActiveTrackingTodo: boolean;
	    governingTodoDueAt?: string;
	    actionableAttentionCount: number;
	    highestAttentionSeverity?: string;
	    hasActiveExpectation: boolean;
	    hasSatisfiedExpectation: boolean;
	    hasOverdueUnmetExpectation: boolean;
	
	    static createFrom(source: any = {}) {
	        return new WorkItemOverviewTrackedItemSignalDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.state = source["state"];
	        this.hasActiveTrackingTodo = source["hasActiveTrackingTodo"];
	        this.governingTodoDueAt = source["governingTodoDueAt"];
	        this.actionableAttentionCount = source["actionableAttentionCount"];
	        this.highestAttentionSeverity = source["highestAttentionSeverity"];
	        this.hasActiveExpectation = source["hasActiveExpectation"];
	        this.hasSatisfiedExpectation = source["hasSatisfiedExpectation"];
	        this.hasOverdueUnmetExpectation = source["hasOverdueUnmetExpectation"];
	    }
	}
	export class WorkItemOverviewTrackedItemDTO {
	    id: string;
	    title: string;
	    itemType?: string;
	    parentItemId?: string;
	    lifecycle: string;
	    currentStepId?: string;
	    currentState?: string;
	    complete: boolean;
	    resolved: boolean;
	    signal: WorkItemOverviewTrackedItemSignalDTO;
	
	    static createFrom(source: any = {}) {
	        return new WorkItemOverviewTrackedItemDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.itemType = source["itemType"];
	        this.parentItemId = source["parentItemId"];
	        this.lifecycle = source["lifecycle"];
	        this.currentStepId = source["currentStepId"];
	        this.currentState = source["currentState"];
	        this.complete = source["complete"];
	        this.resolved = source["resolved"];
	        this.signal = this.convertValues(source["signal"], WorkItemOverviewTrackedItemSignalDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class WorkItemOverviewStageDTO {
	    id: string;
	    key: string;
	    title: string;
	    sequence: number;
	    terminal: boolean;
	
	    static createFrom(source: any = {}) {
	        return new WorkItemOverviewStageDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.key = source["key"];
	        this.title = source["title"];
	        this.sequence = source["sequence"];
	        this.terminal = source["terminal"];
	    }
	}
	export class WorkItemOverviewWorkflowDTO {
	    id: string;
	    definitionId: string;
	    startedAt: string;
	    currentStep: WorkItemOverviewStageDTO;
	    stages: WorkItemOverviewStageDTO[];
	
	    static createFrom(source: any = {}) {
	        return new WorkItemOverviewWorkflowDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.definitionId = source["definitionId"];
	        this.startedAt = source["startedAt"];
	        this.currentStep = this.convertValues(source["currentStep"], WorkItemOverviewStageDTO);
	        this.stages = this.convertValues(source["stages"], WorkItemOverviewStageDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class WorkItemOverviewTrackingDTO {
	    trackedItemCount: number;
	    workflow?: WorkItemOverviewWorkflowDTO;
	    items: WorkItemOverviewTrackedItemDTO[];
	    stateDistribution: WorkItemOverviewStateDistributionDTO;
	    activeFinalTarget?: MonitorPointDTO;
	
	    static createFrom(source: any = {}) {
	        return new WorkItemOverviewTrackingDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.trackedItemCount = source["trackedItemCount"];
	        this.workflow = this.convertValues(source["workflow"], WorkItemOverviewWorkflowDTO);
	        this.items = this.convertValues(source["items"], WorkItemOverviewTrackedItemDTO);
	        this.stateDistribution = this.convertValues(source["stateDistribution"], WorkItemOverviewStateDistributionDTO);
	        this.activeFinalTarget = this.convertValues(source["activeFinalTarget"], MonitorPointDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class WorkItemOverviewItemDTO {
	    workItem: WorkItemDTO;
	    tracking: WorkItemOverviewTrackingDTO;
	    attention: WorkItemOverviewAttentionDTO;
	
	    static createFrom(source: any = {}) {
	        return new WorkItemOverviewItemDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.workItem = this.convertValues(source["workItem"], WorkItemDTO);
	        this.tracking = this.convertValues(source["tracking"], WorkItemOverviewTrackingDTO);
	        this.attention = this.convertValues(source["attention"], WorkItemOverviewAttentionDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class WorkItemOverviewDTO {
	    items: WorkItemOverviewItemDTO[];
	    trackingAvailable: boolean;
	    attentionAvailable: boolean;
	
	    static createFrom(source: any = {}) {
	        return new WorkItemOverviewDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.items = this.convertValues(source["items"], WorkItemOverviewItemDTO);
	        this.trackingAvailable = source["trackingAvailable"];
	        this.attentionAvailable = source["attentionAvailable"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	
	
	
	
	
	export class WorkflowDefinitionDTO {
	    id: string;
	    name: string;
	    version: number;
	    active: boolean;
	    createdAt: string;
	    steps: WorkflowStepDTO[];
	
	    static createFrom(source: any = {}) {
	        return new WorkflowDefinitionDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.version = source["version"];
	        this.active = source["active"];
	        this.createdAt = source["createdAt"];
	        this.steps = this.convertValues(source["steps"], WorkflowStepDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class WorkflowStepDTO {
	    id: string;
	    key: string;
	    title: string;
	    sequence: number;
	    terminal: boolean;
	
	    static createFrom(source: any = {}) {
	        return new WorkflowStepDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.key = source["key"];
	        this.title = source["title"];
	        this.sequence = source["sequence"];
	        this.terminal = source["terminal"];
	    }
	}
	export class WorkflowDTO {
	    id: string;
	    workItemId: string;
	    workflowDefinitionId: string;
	    startedAt: string;
	    currentStep: WorkflowStepDTO;
	    createdAt: string;
	    updatedAt: string;
	    definition: WorkflowDefinitionDTO;
	
	    static createFrom(source: any = {}) {
	        return new WorkflowDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.workItemId = source["workItemId"];
	        this.workflowDefinitionId = source["workflowDefinitionId"];
	        this.startedAt = source["startedAt"];
	        this.currentStep = this.convertValues(source["currentStep"], WorkflowStepDTO);
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.definition = this.convertValues(source["definition"], WorkflowDefinitionDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class WorkItemTrackingDTO {
	    workItemId: string;
	    workflow?: WorkflowDTO;
	    trackedItems: TrackedItemDTO[];
	    relations: TrackedItemRelationDTO[];
	    monitorPoints: MonitorPointDTO[];
	    history: HistoryEventDTO[];
	    attentionIssues: TrackedItemAttentionDTO[];
	
	    static createFrom(source: any = {}) {
	        return new WorkItemTrackingDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.workItemId = source["workItemId"];
	        this.workflow = this.convertValues(source["workflow"], WorkflowDTO);
	        this.trackedItems = this.convertValues(source["trackedItems"], TrackedItemDTO);
	        this.relations = this.convertValues(source["relations"], TrackedItemRelationDTO);
	        this.monitorPoints = this.convertValues(source["monitorPoints"], MonitorPointDTO);
	        this.history = this.convertValues(source["history"], HistoryEventDTO);
	        this.attentionIssues = this.convertValues(source["attentionIssues"], TrackedItemAttentionDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	
	

}

export namespace applicationruntime {
	
	export class SourceProcessingCycleResult {
	    Scanned: number;
	    Claimed: number;
	    Completed: number;
	    Failed: number;
	
	    static createFrom(source: any = {}) {
	        return new SourceProcessingCycleResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Scanned = source["Scanned"];
	        this.Claimed = source["Claimed"];
	        this.Completed = source["Completed"];
	        this.Failed = source["Failed"];
	    }
	}
	export class TrackingReevaluationCycleResult {
	    Scanned: number;
	    Claimed: number;
	    Completed: number;
	    Failed: number;
	
	    static createFrom(source: any = {}) {
	        return new TrackingReevaluationCycleResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Scanned = source["Scanned"];
	        this.Claimed = source["Claimed"];
	        this.Completed = source["Completed"];
	        this.Failed = source["Failed"];
	    }
	}

}

export namespace assetserver {
	
	export class Options {
	    Assets: any;
	    Handler: any;
	
	    static createFrom(source: any = {}) {
	        return new Options(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Assets = source["Assets"];
	        this.Handler = source["Handler"];
	    }
	}

}

export namespace keys {
	
	export class Accelerator {
	    Key: string;
	    Modifiers: string[];
	
	    static createFrom(source: any = {}) {
	        return new Accelerator(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Key = source["Key"];
	        this.Modifiers = source["Modifiers"];
	    }
	}

}

export namespace linux {
	
	export class Messages {
	    WebKit2GTKMinRequired: string;
	
	    static createFrom(source: any = {}) {
	        return new Messages(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.WebKit2GTKMinRequired = source["WebKit2GTKMinRequired"];
	    }
	}
	export class Options {
	    Icon: number[];
	    WindowIsTranslucent: boolean;
	    Messages?: Messages;
	    WebviewGpuPolicy: number;
	    ProgramName: string;
	
	    static createFrom(source: any = {}) {
	        return new Options(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Icon = source["Icon"];
	        this.WindowIsTranslucent = source["WindowIsTranslucent"];
	        this.Messages = this.convertValues(source["Messages"], Messages);
	        this.WebviewGpuPolicy = source["WebviewGpuPolicy"];
	        this.ProgramName = source["ProgramName"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace mac {
	
	export class AboutInfo {
	    Title: string;
	    Message: string;
	    Icon: number[];
	
	    static createFrom(source: any = {}) {
	        return new AboutInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Title = source["Title"];
	        this.Message = source["Message"];
	        this.Icon = source["Icon"];
	    }
	}
	export class Preferences {
	    // Go type: u
	    TabFocusesLinks: any;
	    // Go type: u
	    TextInteractionEnabled: any;
	    // Go type: u
	    FullscreenEnabled: any;
	    ApplicationNameForUserAgent: string;
	    // Go type: u
	    EnableAutoplayWithoutUserAction: any;
	
	    static createFrom(source: any = {}) {
	        return new Preferences(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.TabFocusesLinks = this.convertValues(source["TabFocusesLinks"], null);
	        this.TextInteractionEnabled = this.convertValues(source["TextInteractionEnabled"], null);
	        this.FullscreenEnabled = this.convertValues(source["FullscreenEnabled"], null);
	        this.ApplicationNameForUserAgent = source["ApplicationNameForUserAgent"];
	        this.EnableAutoplayWithoutUserAction = this.convertValues(source["EnableAutoplayWithoutUserAction"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TitleBar {
	    TitlebarAppearsTransparent: boolean;
	    HideTitle: boolean;
	    HideTitleBar: boolean;
	    FullSizeContent: boolean;
	    UseToolbar: boolean;
	    HideToolbarSeparator: boolean;
	
	    static createFrom(source: any = {}) {
	        return new TitleBar(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.TitlebarAppearsTransparent = source["TitlebarAppearsTransparent"];
	        this.HideTitle = source["HideTitle"];
	        this.HideTitleBar = source["HideTitleBar"];
	        this.FullSizeContent = source["FullSizeContent"];
	        this.UseToolbar = source["UseToolbar"];
	        this.HideToolbarSeparator = source["HideToolbarSeparator"];
	    }
	}
	export class Options {
	    TitleBar?: TitleBar;
	    Appearance: string;
	    ContentProtection: boolean;
	    WebviewIsTransparent: boolean;
	    WindowIsTranslucent: boolean;
	    Preferences?: Preferences;
	    DisableZoom: boolean;
	    About?: AboutInfo;
	    DisableEscapeExitsFullscreen: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Options(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.TitleBar = this.convertValues(source["TitleBar"], TitleBar);
	        this.Appearance = source["Appearance"];
	        this.ContentProtection = source["ContentProtection"];
	        this.WebviewIsTransparent = source["WebviewIsTransparent"];
	        this.WindowIsTranslucent = source["WindowIsTranslucent"];
	        this.Preferences = this.convertValues(source["Preferences"], Preferences);
	        this.DisableZoom = source["DisableZoom"];
	        this.About = this.convertValues(source["About"], AboutInfo);
	        this.DisableEscapeExitsFullscreen = source["DisableEscapeExitsFullscreen"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	

}

export namespace menu {
	
	export class MenuItem {
	    Label: string;
	    Role: number;
	    Accelerator?: keys.Accelerator;
	    Type: string;
	    Disabled: boolean;
	    Hidden: boolean;
	    Checked: boolean;
	    SubMenu?: Menu;
	
	    static createFrom(source: any = {}) {
	        return new MenuItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Label = source["Label"];
	        this.Role = source["Role"];
	        this.Accelerator = this.convertValues(source["Accelerator"], keys.Accelerator);
	        this.Type = source["Type"];
	        this.Disabled = source["Disabled"];
	        this.Hidden = source["Hidden"];
	        this.Checked = source["Checked"];
	        this.SubMenu = this.convertValues(source["SubMenu"], Menu);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Menu {
	    Items: MenuItem[];
	
	    static createFrom(source: any = {}) {
	        return new Menu(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Items = this.convertValues(source["Items"], MenuItem);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace options {
	
	export class DragAndDrop {
	    EnableFileDrop: boolean;
	    DisableWebViewDrop: boolean;
	    CSSDropProperty: string;
	    CSSDropValue: string;
	
	    static createFrom(source: any = {}) {
	        return new DragAndDrop(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.EnableFileDrop = source["EnableFileDrop"];
	        this.DisableWebViewDrop = source["DisableWebViewDrop"];
	        this.CSSDropProperty = source["CSSDropProperty"];
	        this.CSSDropValue = source["CSSDropValue"];
	    }
	}
	export class Debug {
	    OpenInspectorOnStartup: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Debug(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.OpenInspectorOnStartup = source["OpenInspectorOnStartup"];
	    }
	}
	export class Experimental {
	
	
	    static createFrom(source: any = {}) {
	        return new Experimental(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class SingleInstanceLock {
	    UniqueId: string;
	
	    static createFrom(source: any = {}) {
	        return new SingleInstanceLock(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.UniqueId = source["UniqueId"];
	    }
	}
	export class RGBA {
	    r: number;
	    g: number;
	    b: number;
	    a: number;
	
	    static createFrom(source: any = {}) {
	        return new RGBA(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.r = source["r"];
	        this.g = source["g"];
	        this.b = source["b"];
	        this.a = source["a"];
	    }
	}
	export class App {
	    Title: string;
	    Width: number;
	    Height: number;
	    DisableResize: boolean;
	    Fullscreen: boolean;
	    Frameless: boolean;
	    MinWidth: number;
	    MinHeight: number;
	    MaxWidth: number;
	    MaxHeight: number;
	    StartHidden: boolean;
	    HideWindowOnClose: boolean;
	    AlwaysOnTop: boolean;
	    BackgroundColour?: RGBA;
	    Assets: any;
	    AssetsHandler: any;
	    AssetServer?: assetserver.Options;
	    Menu?: menu.Menu;
	    LogLevel: number;
	    LogLevelProduction: number;
	    Bind: any[];
	    EnumBind: any[];
	    WindowStartState: number;
	    CSSDragProperty: string;
	    CSSDragValue: string;
	    EnableDefaultContextMenu: boolean;
	    EnableFraudulentWebsiteDetection: boolean;
	    SingleInstanceLock?: SingleInstanceLock;
	    Windows?: windows.Options;
	    Mac?: mac.Options;
	    Linux?: linux.Options;
	    // Go type: Experimental
	    Experimental?: any;
	    Debug: Debug;
	    DragAndDrop?: DragAndDrop;
	    DisablePanicRecovery: boolean;
	    BindingsAllowedOrigins: string;
	
	    static createFrom(source: any = {}) {
	        return new App(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Title = source["Title"];
	        this.Width = source["Width"];
	        this.Height = source["Height"];
	        this.DisableResize = source["DisableResize"];
	        this.Fullscreen = source["Fullscreen"];
	        this.Frameless = source["Frameless"];
	        this.MinWidth = source["MinWidth"];
	        this.MinHeight = source["MinHeight"];
	        this.MaxWidth = source["MaxWidth"];
	        this.MaxHeight = source["MaxHeight"];
	        this.StartHidden = source["StartHidden"];
	        this.HideWindowOnClose = source["HideWindowOnClose"];
	        this.AlwaysOnTop = source["AlwaysOnTop"];
	        this.BackgroundColour = this.convertValues(source["BackgroundColour"], RGBA);
	        this.Assets = source["Assets"];
	        this.AssetsHandler = source["AssetsHandler"];
	        this.AssetServer = this.convertValues(source["AssetServer"], assetserver.Options);
	        this.Menu = this.convertValues(source["Menu"], menu.Menu);
	        this.LogLevel = source["LogLevel"];
	        this.LogLevelProduction = source["LogLevelProduction"];
	        this.Bind = source["Bind"];
	        this.EnumBind = source["EnumBind"];
	        this.WindowStartState = source["WindowStartState"];
	        this.CSSDragProperty = source["CSSDragProperty"];
	        this.CSSDragValue = source["CSSDragValue"];
	        this.EnableDefaultContextMenu = source["EnableDefaultContextMenu"];
	        this.EnableFraudulentWebsiteDetection = source["EnableFraudulentWebsiteDetection"];
	        this.SingleInstanceLock = this.convertValues(source["SingleInstanceLock"], SingleInstanceLock);
	        this.Windows = this.convertValues(source["Windows"], windows.Options);
	        this.Mac = this.convertValues(source["Mac"], mac.Options);
	        this.Linux = this.convertValues(source["Linux"], linux.Options);
	        this.Experimental = this.convertValues(source["Experimental"], null);
	        this.Debug = this.convertValues(source["Debug"], Debug);
	        this.DragAndDrop = this.convertValues(source["DragAndDrop"], DragAndDrop);
	        this.DisablePanicRecovery = source["DisablePanicRecovery"];
	        this.BindingsAllowedOrigins = source["BindingsAllowedOrigins"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	

}

export namespace source {
	
	export class Service {
	
	
	    static createFrom(source: any = {}) {
	        return new Service(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}

}

export namespace sourceprocessing {
	
	export class Processor {
	
	
	    static createFrom(source: any = {}) {
	        return new Processor(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}

}

export namespace trackingreevaluation {
	
	export class Service {
	
	
	    static createFrom(source: any = {}) {
	        return new Service(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}

}

export namespace windows {
	
	export class Messages {
	    InstallationRequired: string;
	    UpdateRequired: string;
	    MissingRequirements: string;
	    Webview2NotInstalled: string;
	    Error: string;
	    FailedToInstall: string;
	    DownloadPage: string;
	    PressOKToInstall: string;
	    ContactAdmin: string;
	    InvalidFixedWebview2: string;
	    WebView2ProcessCrash: string;
	
	    static createFrom(source: any = {}) {
	        return new Messages(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.InstallationRequired = source["InstallationRequired"];
	        this.UpdateRequired = source["UpdateRequired"];
	        this.MissingRequirements = source["MissingRequirements"];
	        this.Webview2NotInstalled = source["Webview2NotInstalled"];
	        this.Error = source["Error"];
	        this.FailedToInstall = source["FailedToInstall"];
	        this.DownloadPage = source["DownloadPage"];
	        this.PressOKToInstall = source["PressOKToInstall"];
	        this.ContactAdmin = source["ContactAdmin"];
	        this.InvalidFixedWebview2 = source["InvalidFixedWebview2"];
	        this.WebView2ProcessCrash = source["WebView2ProcessCrash"];
	    }
	}
	export class ThemeSettings {
	    DarkModeTitleBar: number;
	    DarkModeTitleBarInactive: number;
	    DarkModeTitleText: number;
	    DarkModeTitleTextInactive: number;
	    DarkModeBorder: number;
	    DarkModeBorderInactive: number;
	    LightModeTitleBar: number;
	    LightModeTitleBarInactive: number;
	    LightModeTitleText: number;
	    LightModeTitleTextInactive: number;
	    LightModeBorder: number;
	    LightModeBorderInactive: number;
	
	    static createFrom(source: any = {}) {
	        return new ThemeSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.DarkModeTitleBar = source["DarkModeTitleBar"];
	        this.DarkModeTitleBarInactive = source["DarkModeTitleBarInactive"];
	        this.DarkModeTitleText = source["DarkModeTitleText"];
	        this.DarkModeTitleTextInactive = source["DarkModeTitleTextInactive"];
	        this.DarkModeBorder = source["DarkModeBorder"];
	        this.DarkModeBorderInactive = source["DarkModeBorderInactive"];
	        this.LightModeTitleBar = source["LightModeTitleBar"];
	        this.LightModeTitleBarInactive = source["LightModeTitleBarInactive"];
	        this.LightModeTitleText = source["LightModeTitleText"];
	        this.LightModeTitleTextInactive = source["LightModeTitleTextInactive"];
	        this.LightModeBorder = source["LightModeBorder"];
	        this.LightModeBorderInactive = source["LightModeBorderInactive"];
	    }
	}
	export class Options {
	    ContentProtection: boolean;
	    WebviewIsTransparent: boolean;
	    WindowIsTranslucent: boolean;
	    DisableWindowIcon: boolean;
	    IsZoomControlEnabled: boolean;
	    ZoomFactor: number;
	    DisablePinchZoom: boolean;
	    DisableFramelessWindowDecorations: boolean;
	    WebviewUserDataPath: string;
	    WebviewBrowserPath: string;
	    Theme: number;
	    CustomTheme?: ThemeSettings;
	    BackdropType: number;
	    Messages?: Messages;
	    ResizeDebounceMS: number;
	    WebviewGpuIsDisabled: boolean;
	    WebviewDisableRendererCodeIntegrity: boolean;
	    EnableSwipeGestures: boolean;
	    WindowClassName: string;
	    DLLSearchPaths: number;
	
	    static createFrom(source: any = {}) {
	        return new Options(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ContentProtection = source["ContentProtection"];
	        this.WebviewIsTransparent = source["WebviewIsTransparent"];
	        this.WindowIsTranslucent = source["WindowIsTranslucent"];
	        this.DisableWindowIcon = source["DisableWindowIcon"];
	        this.IsZoomControlEnabled = source["IsZoomControlEnabled"];
	        this.ZoomFactor = source["ZoomFactor"];
	        this.DisablePinchZoom = source["DisablePinchZoom"];
	        this.DisableFramelessWindowDecorations = source["DisableFramelessWindowDecorations"];
	        this.WebviewUserDataPath = source["WebviewUserDataPath"];
	        this.WebviewBrowserPath = source["WebviewBrowserPath"];
	        this.Theme = source["Theme"];
	        this.CustomTheme = this.convertValues(source["CustomTheme"], ThemeSettings);
	        this.BackdropType = source["BackdropType"];
	        this.Messages = this.convertValues(source["Messages"], Messages);
	        this.ResizeDebounceMS = source["ResizeDebounceMS"];
	        this.WebviewGpuIsDisabled = source["WebviewGpuIsDisabled"];
	        this.WebviewDisableRendererCodeIntegrity = source["WebviewDisableRendererCodeIntegrity"];
	        this.EnableSwipeGestures = source["EnableSwipeGestures"];
	        this.WindowClassName = source["WindowClassName"];
	        this.DLLSearchPaths = source["DLLSearchPaths"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

