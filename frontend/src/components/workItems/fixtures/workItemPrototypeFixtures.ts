/**
 * Frontend-only deterministic prototype design fixtures.
 *
 * PURPOSE:
 * Full UI/UX design validation of WorkItem Overview, Baseline Trunk,
 * Branch Information Cards, Dependency Traversal, Health Posture, and Timeline History.
 *
 * BOUNDARY NOTE:
 * This is a frontend-only design prototype fixture dataset.
 * It is completely decoupled from backend Wails / Go domain services and database migrations.
 */

import type {
  ProductWorkItem,
  ProductWorkItemApi,
  ProductWorkItemLifecycle,
} from '../../../api/workItems/productApi.ts';
import type { WorkflowHistoryEvent, WorkflowModel } from '../workflow/workflowTypes.ts';

// ============================================================================
// 1. WORKITEM 1: Production Release / Checkout & Payments Platform (RED)
// ============================================================================
const checkoutPlatformItem: ProductWorkItem = {
  id: 'wi-checkout-platform',
  title: 'Checkout & Payments Platform V2',
  description:
    'Core multi-region payment processing gateway, tokenized checkout flow, PCI compliance sign-off, and canary rollout for global launch.',
  lifecycleState: 'ACTIVE',
  completedAt: null,
  createdAt: '2026-09-10T08:30:00Z',
  updatedAt: '2026-09-18T14:30:00Z',
};

const checkoutPlatformHistory: WorkflowHistoryEvent[] = [
  {
    id: 'cph-1',
    timestamp: 'Sep 18 14:30',
    title: 'Defect #142 Token Leak Detected',
    description:
      'Critical defect identified during automated header sanitization tests. Blocks downstream Frontend Checkout flow.',
    type: 'alert',
    actor: 'Security Scanner',
    nodeId: 'wi-cp-4',
    reason: 'Automated verification detected authorization headers in a serialized error payload.',
    source: 'Security regression suite · run sec-4821',
    evidence: 'payment-error-sanitization.spec.ts · 3 failing assertions',
    affectedEntity: 'Defect #142 Token Leak and three downstream branch items',
  },
  {
    id: 'cph-2',
    timestamp: 'Sep 18 11:20',
    title: 'Health Degraded GREEN → RED',
    description:
      'Outcome health posture downgraded due to Defect #142 blocking the PCI-DSS release gate.',
    type: 'health_change',
    actor: 'Workflow Engine',
    reason:
      'An unresolved critical defect now blocks both checkout staging and compliance sign-off.',
    source: 'Derived from dependency graph evaluation',
    affectedEntity: 'Workflow trunk health posture',
  },
  {
    id: 'cph-3',
    timestamp: 'Sep 18 09:00',
    title: 'Target Deadline Risk Detected',
    description:
      'Release window starts in 3 days (Sep 21). 2 dependent items are still unresolved in Test.',
    type: 'alert',
    actor: 'Delivery Predictor',
    reason: 'The Sep 21 release window is inside the warning threshold with unresolved Test work.',
    source: 'Target-date checkpoint evaluation',
    affectedEntity: 'Stage release checkpoint',
  },
  {
    id: 'cph-4',
    timestamp: 'Sep 17 16:30',
    title: 'Database Schema Migration Completed',
    description:
      'Encrypted payment tokens table migration executed cleanly in staging with zero downtime.',
    type: 'milestone',
    actor: 'Database Admin',
    nodeId: 'wi-cp-1',
    reason: 'Migration verification completed with matching row counts and no lock escalation.',
    source: 'Staging database deployment record',
    affectedEntity: 'Database Schema Migration',
  },
  {
    id: 'cph-14',
    timestamp: 'Sep 17 15:05',
    title: 'Token-Leak Regression Todo Completed',
    description: 'The initial redaction assertions were completed and attached to the defect.',
    type: 'milestone',
    actor: 'Planner Integration',
    nodeId: 'wi-cp-4',
    reason:
      'The remediation needed executable coverage before the patched build could be reviewed.',
    source: 'Planner todo todo-cp4-2',
    affectedEntity: 'Defect #142 Token Leak',
  },
  {
    id: 'cph-5',
    timestamp: 'Sep 17 14:10',
    title: 'Vault Secret Injection Verified',
    description:
      'HSM key rotation sidecar deployed and verified against test hardware security modules.',
    type: 'transition',
    actor: 'Infra Bot',
    nodeId: 'wi-cp-2',
    reason: 'Rotation smoke tests passed against the staging HSM integration.',
    source: 'Infrastructure deployment pipeline',
    affectedEntity: 'Vault Secret Injection',
  },
  {
    id: 'cph-6',
    timestamp: 'Sep 16 10:00',
    title: 'GraphQL Payment API Moved Dev → Test',
    description: 'Core schema and mutation endpoints committed to integration test environment.',
    type: 'transition',
    actor: 'Alice Developer',
    nodeId: 'wi-cp-3',
    reason: 'Implementation criteria passed and the integration-test deployment completed.',
    source: 'Repository merge and CI deployment',
    affectedEntity: 'GraphQL Payment API',
  },
  {
    id: 'cph-7',
    timestamp: 'Sep 14 09:30',
    title: 'Quality Gate Requirements Defined',
    description:
      'PCI-DSS and Production GA sign-off criteria linked to automated verification gates.',
    type: 'dependency',
    actor: 'Security Officer',
    nodeId: 'wi-cp-8',
    reason: 'Release policy requires both automated evidence and explicit security approval.',
    source: 'PCI-DSS release control definition',
    affectedEntity: 'PCI-DSS Compliance Sign-off',
  },
  {
    id: 'cph-9',
    timestamp: 'Sep 13 15:20',
    title: 'Release Target Moved to Sep 21',
    description: 'The Stage checkpoint moved two days to preserve the contracted canary window.',
    type: 'milestone',
    actor: 'Release Manager',
    reason: 'The original window overlapped the payment-network maintenance freeze.',
    source: 'Launch-readiness decision log',
    affectedEntity: 'Workflow target date',
  },
  {
    id: 'cph-10',
    timestamp: 'Sep 12 13:40',
    title: 'Frontend Checkout Branch Introduced',
    description: 'A new experience branch was linked to the payment API and compliance gate.',
    type: 'dependency',
    actor: 'Product Lead',
    nodeId: 'wi-cp-6',
    reason: 'Checkout error recovery required independently trackable acceptance work.',
    source: 'Checkout experience planning session',
    affectedEntity: 'Frontend Checkout Flow',
  },
  {
    id: 'cph-11',
    timestamp: 'Sep 12 09:15',
    title: 'Verification Todo Created',
    description: 'A todo was created to publish the Test-stage evidence bundle.',
    type: 'milestone',
    actor: 'Planner Integration',
    nodeId: 'wi-cp-3',
    reason: 'The release gate requires reproducible API verification evidence.',
    source: 'Planner todo todo-cp3-2',
    affectedEntity: 'GraphQL Payment API',
  },
  {
    id: 'cph-12',
    timestamp: 'Sep 11 17:10',
    title: 'Security Notification Sent',
    description: 'The security officer received the first release-control briefing.',
    type: 'alert',
    actor: 'Notification Service',
    nodeId: 'wi-cp-8',
    reason: 'Compliance reviewers must be present before Stage approval.',
    source: 'Notification delivery record',
    affectedEntity: 'PCI-DSS Compliance Sign-off',
  },
  {
    id: 'cph-13',
    timestamp: 'Sep 11 10:30',
    title: 'Architecture Evidence Linked',
    description: 'The approved payment contract and threat model were attached to the workstream.',
    type: 'dependency',
    actor: 'Lead Architect',
    nodeId: 'wi-cp-3',
    reason: 'Implementation needed an authoritative contract and traceable security assumptions.',
    source: 'Architecture review evidence bundle',
    evidence: 'payment-contract-v2.graphql · threat-model-2026-09',
    affectedEntity: 'GraphQL Payment API',
  },
  {
    id: 'cph-8',
    timestamp: 'Sep 10 08:30',
    title: 'Outcome Initialized',
    description: 'WorkItem created with baseline trunk anchored at Dev stage.',
    type: 'milestone',
    actor: 'Lead Architect',
    reason: 'The V2 payment outcome was approved for active delivery.',
    source: 'Product architecture review',
    affectedEntity: 'Workflow trunk',
  },
];

const checkoutPlatformItemHistories: Record<string, WorkflowHistoryEvent[]> = {
  'wi-cp-3': [
    {
      id: 'ih-cp3-1',
      timestamp: 'Sep 18 10:15',
      title: 'Performance Regressed on Batch Ingestion',
      description: 'P99 latency jumped from 45ms to 180ms during multi-currency stress testing.',
      type: 'health_change',
      actor: 'Load Test Suite',
      source: 'Performance verification run perf-193',
    },
    {
      id: 'ih-cp3-2',
      timestamp: 'Sep 16 10:00',
      title: 'Moved to Test Stage',
      description:
        'Upstream dependencies wi-cp-1 and wi-cp-2 satisfied. Artifact deployed to staging cluster.',
      type: 'transition',
      actor: 'Alice Developer',
      source: 'CI deployment record',
    },
  ],
  'wi-cp-4': [
    {
      id: 'ih-cp4-1',
      timestamp: 'Sep 18 14:30',
      title: 'Critical Blocker Raised',
      description: 'Defect filed: Authorization header leaked in payment gateway error payload.',
      type: 'alert',
      actor: 'Security Audit',
      source: 'Security regression suite sec-4821',
      reason: 'The scanner reproduced credential material in a serialized gateway error.',
      evidence: 'payment-error-sanitization.spec.ts · assertion 3',
    },
    {
      id: 'ih-cp4-2',
      timestamp: 'Sep 18 13:12',
      title: 'GraphQL Dependency Linked',
      description: 'The defect was linked downstream of the payment API verification branch.',
      type: 'dependency',
      actor: 'Workflow Engine',
      source: 'Dependency graph update',
      reason: 'The failing response originates in the GraphQL gateway error mapper.',
    },
    {
      id: 'ih-cp4-3',
      timestamp: 'Sep 18 11:40',
      title: 'Sanitizer Failure Reproduced',
      description: 'The security test reproduced the raw bearer-token leak in staging.',
      type: 'alert',
      actor: 'Security Scanner',
      source: 'Security run sec-4821',
      evidence: 'trace correlation sec-4821-17',
    },
    {
      id: 'ih-cp4-4',
      timestamp: 'Sep 17 16:20',
      title: 'Target Changed Sep 17 → Sep 18',
      description: 'Remediation review moved to the next security verification window.',
      type: 'milestone',
      actor: 'Ross · Security Lead',
      source: 'Release-control decision log',
      reason: 'The first patch did not preserve trace correlation metadata.',
    },
    {
      id: 'ih-cp4-5',
      timestamp: 'Sep 17 10:10',
      title: 'Defect Item Created',
      description: 'Defect #142 was created from automated staging evidence.',
      type: 'milestone',
      actor: 'Rally Import',
      source: 'Rally RALLY-4821',
      evidence: 'Initial scan summary and failing request sample',
    },
  ],
  'wi-cp-6': [
    {
      id: 'ih-cp6-1',
      timestamp: 'Sep 18 14:35',
      title: 'Execution Blocked by Upstream Defect #142',
      description: 'Checkout form integration paused until API sanitization patch lands.',
      type: 'alert',
      actor: 'Frontend Lead',
      source: 'Staging integration checkpoint',
    },
  ],
};

const checkoutPlatformWorkflow: WorkflowModel = {
  workItemId: 'wi-checkout-platform',
  title: 'Checkout & Payments Platform V2',
  description: checkoutPlatformItem.description,
  aggregateHealth: 'red',
  hasAuthoritativeHealth: true,
  healthLabel: 'BLOCKED · CRITICAL',
  hasTargetMarker: true,
  targetStageId: 'stage',
  targetDate: '2026-09-21',
  targetLabel: 'Target: Sep 21 (Stage)',
  targetTimingLabel: '3 days remaining',
  statusReason:
    'GraphQL API 2 days behind schedule; Defect #142 blocks Frontend Checkout and Release Gate. Only 3 days remaining before release window.',
  origin: {
    label: 'Payments Platform V2 launch plan',
    kind: 'Product initiative',
    detail: 'Created from the Sep 10 architecture and launch-readiness review.',
    observedAt: 'Sep 10 · 8:30 AM',
    linkedBy: 'Lead Architect',
  },
  nextMilestone: {
    label: 'Security patch verification',
    targetAt: 'Sep 19 · 10:00 AM',
    timingLabel: '19 hours remaining',
  },
  trunk: {
    currentStageId: 'test',
    health: 'red',
    progressPercent: 65,
    label: 'Blocked · Test Verification',
  },
  nodes: [
    {
      id: 'wi-cp-1',
      title: 'Database Schema Migration',
      summary: 'Encrypted tokens table & index',
      itemType: 'story',
      stageId: 'dev',
      health: 'green',
      isResolved: true,
      lane: -1,
      dependencies: [],
      statusReason: 'Migration applied and verified in staging DB',
      latestActivity: 'Applied on Sep 17 16:30',
    },
    {
      id: 'wi-cp-2',
      title: 'Vault Secret Injection',
      summary: 'HSM key rotation sidecar daemon',
      itemType: 'task',
      stageId: 'dev',
      health: 'green',
      isResolved: true,
      lane: 1,
      dependencies: [],
      statusReason: 'Key rotation sidecar daemon verified',
      latestActivity: 'Completed on Sep 17 14:10',
    },
    {
      id: 'wi-cp-3',
      title: 'GraphQL Payment API',
      summary: 'Unified checkout query & mutation',
      signal: '1 alert',
      itemType: 'story',
      stageId: 'test',
      health: 'yellow',
      isResolved: false,
      lane: -1,
      targetStageId: 'stage',
      targetDate: 'Sep 20',
      dependencies: ['wi-cp-1', 'wi-cp-2'],
      statusReason:
        'Testing is 2 days behind expected progress. Latency regression under investigation.',
      latestActivity: 'Load test executed Sep 18 10:15',
      description:
        'Owns the unified payment query and mutation surface, including tokenization, authorization, idempotency, and multi-currency response handling.',
      stateLabel: 'Verification in progress',
      source: {
        label: 'Checkout platform architecture plan',
        kind: 'Architecture workstream',
        detail: 'Imported from the V2 API delivery outline and repository milestone.',
        reference: 'RALLY-3911',
        why: 'Provides the payment contract used by the checkout frontend.',
        observedAt: 'Sep 10 architecture review',
        linkedBy: 'Alice Developer',
      },
      nextMilestone: {
        label: 'Latency regression decision',
        targetAt: 'Sep 19 · 9:00 AM',
        timingLabel: '18 hours remaining',
      },
      relatedTodos: [
        {
          id: 'todo-cp3-1',
          title: 'Profile multi-currency batch latency',
          status: 'OPEN',
          timing: 'Today · 4:00 PM',
        },
        {
          id: 'todo-cp3-2',
          title: 'Publish Test-stage evidence bundle',
          status: 'OPEN',
          timing: 'Tomorrow · 9:00 AM',
        },
      ],
      relatedNotifications: [
        {
          id: 'note-cp3-1',
          title: 'P99 latency exceeded verification threshold',
          severity: 'ATTENTION',
          status: 'OPEN',
        },
      ],
      linkedItemIds: ['wi-cp-1', 'wi-cp-2', 'wi-cp-4', 'wi-cp-6'],
    },
    {
      id: 'wi-cp-4',
      title: 'Defect #142 Token Leak',
      summary: 'Sanitize authorization in error response',
      signal: 'Blocks 3 items',
      itemType: 'defect',
      stageId: 'test',
      health: 'red',
      isResolved: false,
      lane: 1,
      dependencies: ['wi-cp-3'],
      targetStageId: 'test',
      targetDate: 'Sep 18',
      statusReason:
        'Critical security blocker. Error response exposes raw bearer tokens in stack trace.',
      latestActivity: 'Flagged on Sep 18 14:30',
      description:
        'Removes raw authorization material from gateway error serialization, adds regression coverage, and verifies that tracing retains correlation without credential leakage.',
      stateLabel: 'Critical blocker · remediation active',
      source: {
        label: 'Security regression suite',
        kind: 'Automated evidence',
        detail: 'Run sec-4821 · payment-error-sanitization.spec.ts',
        reference: 'RALLY-4821',
        why: 'Token leakage was found during the automated security scan.',
        observedAt: 'Sep 18 · 2:30 PM',
        linkedBy: 'Security Scanner',
      },
      evidence: '3 failing sanitizer assertions · trace correlation sec-4821-17',
      nextMilestone: {
        label: 'Patched build ready for security rerun',
        targetAt: 'Sep 18 · 8:00 PM',
        timingLabel: '5 hours remaining',
      },
      relatedTodos: [
        {
          id: 'todo-cp4-1',
          title: 'Redact authorization headers in error mapper',
          status: 'OPEN',
          timing: 'Today · 6:00 PM',
        },
        {
          id: 'todo-cp4-2',
          title: 'Add token-leak regression assertions',
          status: 'COMPLETED',
          timing: 'Completed 3:05 PM',
        },
      ],
      relatedNotifications: [
        {
          id: 'note-cp4-1',
          title: 'Critical credential exposure blocks release gates',
          severity: 'CRITICAL',
          status: 'OPEN',
          timing: 'Today · 2:32 PM',
        },
        {
          id: 'note-cp4-2',
          title: 'Security officer acknowledgement recorded',
          severity: 'ATTENTION',
          status: 'ACKNOWLEDGED',
          timing: 'Today · 2:38 PM',
        },
      ],
      linkedItemIds: ['wi-cp-3', 'wi-cp-5', 'wi-cp-6', 'wi-cp-8'],
    },
    {
      id: 'wi-cp-5',
      title: 'Regression Test Suite',
      summary: 'Automated 3D-Secure assertions',
      itemType: 'task',
      stageId: 'test',
      health: 'yellow',
      isResolved: false,
      lane: 2,
      dependencies: ['wi-cp-4'],
      statusReason: 'Waiting on Defect #142 resolution before completing full suite run.',
      latestActivity: 'Scheduled after defect fix',
    },
    {
      id: 'wi-cp-6',
      title: 'Frontend Checkout Flow',
      summary: 'One-click payment form & error banner',
      itemType: 'story',
      stageId: 'stage',
      health: 'red',
      isResolved: false,
      lane: -1,
      dependencies: ['wi-cp-3', 'wi-cp-4', 'wi-cp-5'],
      statusReason:
        'Blocked by upstream Defect #142 and GraphQL API test lag; also gated on Regression Test Suite sign-off before this checkout flow can be marked ready.',
      latestActivity: 'UI build stalled in Staging',
      description:
        'Delivers the one-click checkout form, recoverable error states, payment-method selection, and client-side correlation required for support diagnostics.',
      stateLabel: 'Blocked · awaiting upstream patch',
      source: {
        label: 'Checkout experience specification',
        kind: 'Product requirement',
        detail: 'Linked to the GraphQL payment contract and staging acceptance plan.',
      },
      nextMilestone: {
        label: 'Resume staging integration',
        targetAt: 'Sep 19 · 1:00 PM',
        timingLabel: 'Blocked until Defect #142 passes',
      },
      relatedTodos: [
        {
          id: 'todo-cp6-1',
          title: 'Verify sanitized error banner payload',
          status: 'OPEN',
          timing: 'After security rerun',
        },
      ],
      relatedNotifications: [
        {
          id: 'note-cp6-1',
          title: 'Staging integration paused by upstream security defect',
          severity: 'CRITICAL',
          status: 'OPEN',
        },
      ],
      linkedItemIds: ['wi-cp-3', 'wi-cp-4', 'wi-cp-7', 'wi-cp-8'],
    },
    {
      id: 'wi-cp-7',
      title: 'Staging Performance Canary',
      summary: '10k RPS load test on staging cluster',
      itemType: 'task',
      stageId: 'stage',
      health: 'gray',
      isResolved: false,
      lane: 1,
      dependencies: ['wi-cp-5', 'wi-cp-6'],
      statusReason: 'Queued pending frontend and regression suite completion.',
      latestActivity: 'Pipeline queued',
    },
    {
      id: 'wi-cp-8',
      title: 'PCI-DSS Compliance Sign-off',
      summary: 'Security officer audit & sign-off',
      itemType: 'gate',
      stageId: 'stage',
      health: 'red',
      isResolved: false,
      lane: -2,
      dependencies: ['wi-cp-4', 'wi-cp-6'],
      statusReason: 'Cannot sign off while Defect #142 token leakage remains open.',
      latestActivity: 'Audit paused on Sep 18 14:40',
    },
    {
      id: 'wi-cp-9',
      title: 'Canary Traffic Ramp',
      summary: '5% production routing via Envoy',
      itemType: 'story',
      stageId: 'adoption',
      health: 'gray',
      isResolved: false,
      lane: -1,
      dependencies: ['wi-cp-7', 'wi-cp-8'],
      statusReason: 'Waiting for staging performance and security gates to pass.',
      latestActivity: 'Flighting manifest drafted',
    },
    {
      id: 'wi-cp-10',
      title: 'Production GA Release Gate',
      summary: 'Global availability go/no-go',
      itemType: 'gate',
      stageId: 'production',
      health: 'gray',
      isResolved: false,
      lane: 0,
      dependencies: ['wi-cp-9'],
      statusReason: 'Ultimate launch gate. Expected completion on Sep 21.',
      latestActivity: 'Scheduled for release window',
    },
  ],
  edges: [
    { from: 'wi-cp-1', to: 'wi-cp-3' },
    { from: 'wi-cp-2', to: 'wi-cp-3' },
    { from: 'wi-cp-3', to: 'wi-cp-4' },
    { from: 'wi-cp-3', to: 'wi-cp-6' },
    { from: 'wi-cp-4', to: 'wi-cp-5' },
    { from: 'wi-cp-4', to: 'wi-cp-6' },
    { from: 'wi-cp-4', to: 'wi-cp-8' },
    // Frontend Checkout Flow is also gated on Regression Test Suite
    // sign-off — a real gating relationship whose source (Verification)
    // sits later in the normal delivery flow than its target (Frontend),
    // i.e. a genuine backward/exception dependency in the fixture.
    { from: 'wi-cp-5', to: 'wi-cp-6' },
    { from: 'wi-cp-5', to: 'wi-cp-7' },
    { from: 'wi-cp-6', to: 'wi-cp-7' },
    { from: 'wi-cp-6', to: 'wi-cp-8' },
    { from: 'wi-cp-7', to: 'wi-cp-9' },
    { from: 'wi-cp-8', to: 'wi-cp-9' },
    { from: 'wi-cp-9', to: 'wi-cp-10' },
  ],
  historyEvents: checkoutPlatformHistory,
  itemHistories: checkoutPlatformItemHistories,
  isRealData: false,
};

// ============================================================================
// 2. WORKITEM 2: Distributed Systems Learning Plan (YELLOW)
// ============================================================================
const distSystemsItem: ProductWorkItem = {
  id: 'wi-dist-systems',
  title: 'Distributed Consensus & Raft Learning Curriculum',
  description:
    'Core distributed systems study: Raft consensus paper, leader election, log replication lab, crash recovery, and final systems defense.',
  lifecycleState: 'ACTIVE',
  completedAt: null,
  createdAt: '2026-09-01T10:00:00Z',
  updatedAt: '2026-09-18T12:00:00Z',
};

const distSystemsHistory: WorkflowHistoryEvent[] = [
  {
    id: 'dsh-1',
    timestamp: 'Sep 18 12:00',
    title: 'Lab 2B Split-Brain Defect Identified',
    description: 'Test 2B fails partition healing test with duplicate commit index.',
    type: 'alert',
    actor: 'Lab Autograder',
    nodeId: 'wi-ds-5',
  },
  {
    id: 'dsh-2',
    timestamp: 'Sep 17 18:00',
    title: 'Lab 2B Log Replication Behind Schedule',
    description: 'Term election logic complete, but RPC retry backoff is causing timeout flakes.',
    type: 'health_change',
    actor: 'Self Tracker',
    nodeId: 'wi-ds-4',
  },
  {
    id: 'dsh-3',
    timestamp: 'Sep 14 15:30',
    title: 'Lab 2A Leader Election Passed',
    description: 'All 10 election stress tests passing consistently across 50 runs.',
    type: 'milestone',
    actor: 'Autograder',
    nodeId: 'wi-ds-2',
  },
  {
    id: 'dsh-4',
    timestamp: 'Sep 05 11:00',
    title: 'Raft Paper Reading Notes Finished',
    description: 'Reviewed safety argument and Fig 8 corner cases.',
    type: 'milestone',
    actor: 'Student',
    nodeId: 'wi-ds-1',
  },
];

const distSystemsWorkflow: WorkflowModel = {
  workItemId: 'wi-dist-systems',
  title: 'Distributed Consensus & Raft Learning Curriculum',
  description: distSystemsItem.description,
  aggregateHealth: 'yellow',
  hasAuthoritativeHealth: true,
  healthLabel: 'AT RISK · LAG',
  hasTargetMarker: true,
  targetStageId: 'test',
  targetDate: '2026-09-29',
  targetLabel: 'Target: Sep 29 (Exam Gate)',
  targetTimingLabel: '11 days remaining',
  statusReason:
    'Lab 2B Log Replication running 3 days late; network partition test failures require re-reading Section 5.4.',
  origin: {
    label: 'Distributed systems learning plan',
    kind: 'Personal curriculum',
    detail: 'Structured from the Raft paper, lab sequence, and final defense rubric.',
  },
  nextMilestone: {
    label: 'Partition-healing autograder rerun',
    targetAt: 'Sep 20 · 6:00 PM',
    timingLabel: '2 days remaining',
  },
  trunk: {
    currentStageId: 'dev',
    health: 'yellow',
    progressPercent: 45,
    label: 'At Risk · Implementation Phase',
  },
  nodes: [
    {
      id: 'wi-ds-1',
      title: 'Paper Review: Raft Extended',
      summary: 'Analyze safety proofs and election rules',
      itemType: 'story',
      stageId: 'queue',
      health: 'green',
      isResolved: true,
      lane: -1,
      dependencies: [],
      statusReason: 'Finished reading and synthesized summary notes',
      latestActivity: 'Completed Sep 05',
    },
    {
      id: 'wi-ds-2',
      title: 'Lab 2A Leader Election',
      summary: 'Election timeout & heartbeat ticker',
      itemType: 'task',
      stageId: 'dev',
      health: 'green',
      isResolved: true,
      lane: -1,
      dependencies: ['wi-ds-1'],
      statusReason: 'All election test cases passing (50/50 runs)',
      latestActivity: 'Passed Sep 14',
    },
    {
      id: 'wi-ds-3',
      title: 'Fix Split-Brain Flaw',
      summary: 'Prevent stale leader accepting entries',
      itemType: 'defect',
      stageId: 'dev',
      health: 'green',
      isResolved: true,
      lane: 1,
      dependencies: ['wi-ds-2'],
      statusReason: 'Resolved by discarding heartbeats with lower term',
      latestActivity: 'Resolved Sep 15',
    },
    {
      id: 'wi-ds-4',
      title: 'Lab 2B Log Replication',
      summary: 'AppendEntries RPC & log matching',
      itemType: 'task',
      stageId: 'dev',
      health: 'yellow',
      isResolved: false,
      lane: -1,
      targetStageId: 'test',
      dependencies: ['wi-ds-3'],
      statusReason: 'Behind schedule; intermittent timeout flakes during partition recovery',
      latestActivity: 'Investigating matchIndex tracking',
      nextMilestone: {
        label: 'Partition recovery fix review',
        targetAt: 'Sep 15 · 6:00 PM',
        timingLabel: 'Overdue by 3 days',
      },
    },
    {
      id: 'wi-ds-5',
      title: 'Uncommitted Entries Drop Bug',
      summary: 'Bug in uncommitted entry truncation',
      itemType: 'defect',
      stageId: 'test',
      health: 'red',
      isResolved: false,
      lane: 1,
      dependencies: ['wi-ds-4'],
      statusReason: 'Test 2B fails when network partition isolates prior leader',
      latestActivity: 'Failed autograder run Sep 18',
    },
    {
      id: 'wi-ds-6',
      title: 'Lab 2C State Persistence',
      summary: 'Crash recovery with persistent storage',
      itemType: 'task',
      stageId: 'test',
      health: 'gray',
      isResolved: false,
      lane: -1,
      dependencies: ['wi-ds-4'],
      statusReason: 'Prerequisite Lab 2B not yet passing',
      latestActivity: 'Drafted skeleton',
    },
    {
      id: 'wi-ds-7',
      title: 'Linearizability Jepsen Suite',
      summary: 'Automated chaos test suite',
      itemType: 'story',
      stageId: 'stage',
      health: 'gray',
      isResolved: false,
      lane: -1,
      dependencies: ['wi-ds-5', 'wi-ds-6'],
      statusReason: 'Queued for staging phase',
      latestActivity: 'Test harness configured',
    },
    {
      id: 'wi-ds-8',
      title: 'Final Systems Defense Gate',
      summary: 'Milestone oral presentation & code defense',
      itemType: 'gate',
      stageId: 'production',
      health: 'gray',
      isResolved: false,
      lane: 0,
      dependencies: ['wi-ds-7'],
      statusReason: 'Final curriculum completion gate (Sep 29)',
      latestActivity: 'Presentation date locked',
    },
  ],
  edges: [
    { from: 'wi-ds-1', to: 'wi-ds-2' },
    { from: 'wi-ds-2', to: 'wi-ds-3' },
    { from: 'wi-ds-3', to: 'wi-ds-4' },
    { from: 'wi-ds-4', to: 'wi-ds-5' },
    { from: 'wi-ds-4', to: 'wi-ds-6' },
    { from: 'wi-ds-5', to: 'wi-ds-7' },
    { from: 'wi-ds-6', to: 'wi-ds-7' },
    { from: 'wi-ds-7', to: 'wi-ds-8' },
  ],
  historyEvents: distSystemsHistory,
  isRealData: false,
};

// ============================================================================
// 3. WORKITEM 3: LOB Client Self-Service Onboarding (GREEN)
// ============================================================================
const lobOnboardingItem: ProductWorkItem = {
  id: 'wi-lob-onboarding',
  title: 'LOB Client Automated Onboarding Portal',
  description:
    'Streamlined enterprise tenant registration, Okta SSO connection wizard, webhook broker provisioning, and self-serve API keys.',
  lifecycleState: 'ACTIVE',
  completedAt: null,
  createdAt: '2026-09-05T09:00:00Z',
  updatedAt: '2026-09-18T10:00:00Z',
};

const lobOnboardingHistory: WorkflowHistoryEvent[] = [
  {
    id: 'loh-1',
    timestamp: 'Sep 18 10:00',
    title: 'Staging E2E Smoke Tests Passing',
    description: '100% automated signup and key provisioning scenarios pass in staging cluster.',
    type: 'milestone',
    actor: 'CI Pipeline',
    nodeId: 'wi-lo-4',
  },
  {
    id: 'loh-2',
    timestamp: 'Sep 16 14:00',
    title: 'API Credentials Module Resolved',
    description: 'Automated token issuance with cryptographic hashing completed and verified.',
    type: 'transition',
    actor: 'Bob Engineer',
    nodeId: 'wi-lo-3',
  },
  {
    id: 'loh-3',
    timestamp: 'Sep 12 11:30',
    title: 'Tenant SSO Wizard Completed',
    description: 'SAML 2.0 and OIDC connection wizard merged into main.',
    type: 'transition',
    actor: 'Carol Dev',
    nodeId: 'wi-lo-1',
  },
];

const lobOnboardingWorkflow: WorkflowModel = {
  workItemId: 'wi-lob-onboarding',
  title: 'LOB Client Automated Onboarding Portal',
  description: lobOnboardingItem.description,
  aggregateHealth: 'green',
  hasAuthoritativeHealth: true,
  healthLabel: 'ON TRACK · HEALTHY',
  hasTargetMarker: true,
  targetStageId: 'adoption',
  targetDate: '2026-10-15',
  targetLabel: 'Target: Oct 15 (Adoption)',
  statusReason:
    'All staging smoke tests passing with 99.8% reliability. Tenant cohort 1 flighting starts next week.',
  origin: {
    label: 'LOB onboarding program',
    kind: 'Customer enablement initiative',
    detail: 'Created from the enterprise onboarding service blueprint.',
  },
  nextMilestone: {
    label: 'Cohort 1 flighting readiness review',
    targetAt: 'Sep 23 · 11:00 AM',
    timingLabel: '5 days remaining',
  },
  trunk: {
    currentStageId: 'stage',
    health: 'green',
    progressPercent: 80,
    label: 'On Track · Staging Validation',
  },
  nodes: [
    {
      id: 'wi-lo-1',
      title: 'Tenant SSO Connection Wizard',
      summary: 'SAML 2.0 & OIDC identity integration',
      itemType: 'story',
      stageId: 'dev',
      health: 'green',
      isResolved: true,
      lane: -1,
      dependencies: [],
      statusReason: 'Merged and verified against Okta / Azure AD',
      latestActivity: 'Completed Sep 12',
    },
    {
      id: 'wi-lo-2',
      title: 'Webhook Dispatch Broker',
      summary: 'Guaranteed at-least-once event delivery',
      itemType: 'task',
      stageId: 'dev',
      health: 'green',
      isResolved: true,
      lane: 1,
      dependencies: [],
      statusReason: 'Kafka-backed dead letter queue tested',
      latestActivity: 'Completed Sep 14',
    },
    {
      id: 'wi-lo-3',
      title: 'Self-Serve API Credentials',
      summary: 'Granular scoped API key generator',
      itemType: 'story',
      stageId: 'stage',
      health: 'green',
      isResolved: true,
      lane: -1,
      dependencies: ['wi-lo-1', 'wi-lo-2'],
      statusReason: 'Passing staging penetration tests',
      latestActivity: 'Completed Sep 16',
    },
    {
      id: 'wi-lo-4',
      title: 'Staging E2E Smoke Tests',
      summary: 'Automated signup end-to-end suite',
      itemType: 'task',
      stageId: 'stage',
      health: 'green',
      isResolved: false,
      lane: 1,
      targetStageId: 'adoption',
      dependencies: ['wi-lo-3'],
      statusReason: 'Running hourly validation runs; 0 regressions detected',
      latestActivity: 'Passing run Sep 18 10:00',
    },
    {
      id: 'wi-lo-5',
      title: 'Beta Flighting Gate',
      summary: 'First 5 corporate tenants onboarded',
      itemType: 'gate',
      stageId: 'adoption',
      health: 'gray',
      isResolved: false,
      lane: 0,
      dependencies: ['wi-lo-4'],
      statusReason: 'Scheduled to open Oct 15 upon customer agreement',
      latestActivity: 'Tenant agreements signed',
    },
  ],
  edges: [
    { from: 'wi-lo-1', to: 'wi-lo-3' },
    { from: 'wi-lo-2', to: 'wi-lo-3' },
    { from: 'wi-lo-3', to: 'wi-lo-4' },
    { from: 'wi-lo-4', to: 'wi-lo-5' },
  ],
  historyEvents: lobOnboardingHistory,
  isRealData: false,
};

// ============================================================================
// 4. WORKITEM 4: Completed SOC-2 Compliance Audit (COMPLETED in Closed section)
// ============================================================================
const securityAuditItem: ProductWorkItem = {
  id: 'wi-security-audit',
  title: 'SOC-2 Type II Annual Security Certification',
  description:
    'Comprehensive audit logging infrastructure, automated evidence collection, external penetration test, and final auditor attestation report.',
  lifecycleState: 'COMPLETED',
  completedAt: '2026-09-15T18:00:00Z',
  createdAt: '2026-08-01T08:00:00Z',
  updatedAt: '2026-09-15T18:00:00Z',
};

const securityAuditHistory: WorkflowHistoryEvent[] = [
  {
    id: 'sah-1',
    timestamp: 'Sep 15 18:00',
    title: 'WorkItem Completed & Certified',
    description:
      'Final auditor attestation report signed with zero exceptions. Transitioned to COMPLETED.',
    type: 'milestone',
    actor: 'Lead Auditor',
    nodeId: 'wi-sa-5',
  },
  {
    id: 'sah-2',
    timestamp: 'Sep 12 16:00',
    title: 'Evidence Review Gate Approved',
    description: 'All 142 controls verified with immutable cryptographic evidence records.',
    type: 'milestone',
    actor: 'Compliance Committee',
    nodeId: 'wi-sa-4',
  },
  {
    id: 'sah-3',
    timestamp: 'Sep 01 10:00',
    title: 'Penetration Test Report Delivered',
    description:
      'External security assessment completed with 0 critical and 0 high vulnerabilities.',
    type: 'transition',
    actor: 'Red Team Lead',
    nodeId: 'wi-sa-3',
  },
  {
    id: 'sah-4',
    timestamp: 'Aug 15 14:00',
    title: 'Secret Rotation Audit Gap Resolved',
    description: 'Defect resolved: Automated rotation event hooks now emit signed audit logs.',
    type: 'transition',
    actor: 'Dev Team',
    nodeId: 'wi-sa-2',
  },
  {
    id: 'sah-5',
    timestamp: 'Aug 05 09:00',
    title: 'Immutable Audit Pipeline Deployed',
    description: 'Append-only audit log storage configured with WORM storage enforcement.',
    type: 'transition',
    actor: 'Infra Engineer',
    nodeId: 'wi-sa-1',
  },
];

const securityAuditWorkflow: WorkflowModel = {
  workItemId: 'wi-security-audit',
  title: 'SOC-2 Type II Annual Security Certification',
  description: securityAuditItem.description,
  aggregateHealth: 'green',
  hasAuthoritativeHealth: true,
  healthLabel: 'COMPLETED · CERTIFIED',
  hasTargetMarker: false,
  targetDate: '2026-09-15',
  targetLabel: 'Completed on Sep 15, 2026',
  statusReason: 'All 4 audit gates approved. SOC-2 Type II report received with zero exceptions.',
  origin: {
    label: 'Annual security certification plan',
    kind: 'Compliance program',
    detail: 'Initiated from the 2026 SOC-2 Type II audit schedule.',
  },
  nextMilestone: {
    label: 'Archive evidence retention review',
    targetAt: 'Oct 15 · 9:00 AM',
    timingLabel: 'Post-completion checkpoint',
  },
  trunk: {
    currentStageId: 'production',
    health: 'green',
    progressPercent: 100,
    label: 'Completed · Certified',
  },
  nodes: [
    {
      id: 'wi-sa-1',
      title: 'Immutable Audit Log Pipeline',
      summary: 'Append-only log storage with WORM',
      itemType: 'task',
      stageId: 'dev',
      health: 'green',
      isResolved: true,
      lane: -1,
      dependencies: [],
      statusReason: 'Production deployment verified',
      latestActivity: 'Completed Aug 05',
    },
    {
      id: 'wi-sa-2',
      title: 'Secret Rotation Audit Gap Fix',
      summary: 'Signed audit emission on rotation',
      itemType: 'defect',
      stageId: 'dev',
      health: 'green',
      isResolved: true,
      lane: 1,
      dependencies: ['wi-sa-1'],
      statusReason: 'Security gap closed and re-tested',
      latestActivity: 'Completed Aug 15',
    },
    {
      id: 'wi-sa-3',
      title: 'Third-Party Penetration Test',
      summary: 'External white-box security review',
      itemType: 'task',
      stageId: 'test',
      health: 'green',
      isResolved: true,
      lane: -1,
      dependencies: ['wi-sa-2'],
      statusReason: 'Final assessment delivered (0 critical/high)',
      latestActivity: 'Delivered Sep 01',
    },
    {
      id: 'wi-sa-4',
      title: 'Evidence Review Gate',
      summary: '142 control criteria verification',
      itemType: 'gate',
      stageId: 'stage',
      health: 'green',
      isResolved: true,
      lane: -1,
      dependencies: ['wi-sa-3'],
      statusReason: 'Approved by internal compliance team',
      latestActivity: 'Approved Sep 12',
    },
    {
      id: 'wi-sa-5',
      title: 'Final Attestation Sign-off Gate',
      summary: 'SOC-2 Type II formal auditor report',
      itemType: 'gate',
      stageId: 'production',
      health: 'green',
      isResolved: true,
      lane: 0,
      dependencies: ['wi-sa-4'],
      statusReason: 'Certified and archived in corporate records',
      latestActivity: 'Signed Sep 15',
    },
  ],
  edges: [
    { from: 'wi-sa-1', to: 'wi-sa-2' },
    { from: 'wi-sa-2', to: 'wi-sa-3' },
    { from: 'wi-sa-3', to: 'wi-sa-4' },
    { from: 'wi-sa-4', to: 'wi-sa-5' },
  ],
  historyEvents: securityAuditHistory,
  isRealData: false,
};

// ============================================================================
// PROTOTYPE DATA REGISTRY & IN-MEMORY STORE
// ============================================================================
export const PROTOTYPE_WORK_ITEMS: ProductWorkItem[] = [
  checkoutPlatformItem,
  distSystemsItem,
  lobOnboardingItem,
  securityAuditItem,
];

export const PROTOTYPE_WORKFLOW_MODELS: Record<string, WorkflowModel> = {
  'wi-checkout-platform': checkoutPlatformWorkflow,
  'wi-dist-systems': distSystemsWorkflow,
  'wi-lob-onboarding': lobOnboardingWorkflow,
  'wi-security-audit': securityAuditWorkflow,
};

// In-memory store for prototype mutations (create, edit, lifecycle actions)
let inMemoryItems: ProductWorkItem[] = [...PROTOTYPE_WORK_ITEMS];
const inMemoryModels: Record<string, WorkflowModel> = { ...PROTOTYPE_WORKFLOW_MODELS };

export function getPrototypeWorkflowModel(workItemId: string): WorkflowModel {
  if (inMemoryModels[workItemId]) {
    return inMemoryModels[workItemId];
  }

  // Fallback for user-created items during prototype session
  const fallbackItem = inMemoryItems.find((i) => i.id === workItemId);
  return {
    workItemId,
    title: fallbackItem?.title ?? 'User Created Outcome',
    description: fallbackItem?.description ?? 'Prototype user-created outcome',
    aggregateHealth: 'green',
    hasAuthoritativeHealth: true,
    healthLabel: 'ON TRACK',
    hasTargetMarker: false,
    targetDate: null,
    targetLabel: 'No delivery target configured',
    statusReason: 'Freshly created outcome baseline.',
    trunk: {
      currentStageId: fallbackItem?.lifecycleState === 'COMPLETED' ? 'production' : 'dev',
      health: 'green',
      progressPercent: fallbackItem?.lifecycleState === 'COMPLETED' ? 100 : 25,
      label: fallbackItem?.lifecycleState ?? 'ACTIVE',
    },
    nodes: [],
    edges: [],
    historyEvents: [
      {
        id: `ev-${Date.now()}`,
        timestamp: 'Just now',
        title: 'WorkItem Created in Prototype Mode',
        description: 'Initialized in local interactive session.',
        type: 'milestone',
        actor: 'User',
      },
    ],
    isRealData: false,
  };
}

/**
 * In-memory interactive API adapter for prototype mode.
 * Satisfies ProductWorkItemApi without calling Wails / Go backend.
 */
export const prototypeWorkItemApi: ProductWorkItemApi = {
  list: async (): Promise<ProductWorkItem[]> => {
    return [...inMemoryItems];
  },

  // Fixture-only: prototype mode carries no authoritative summaries.
  overview: async () => ({
    items: inMemoryItems.map((workItem) => ({ workItem, tracking: null, attention: null })),
  }),

  create: async (input: { title: string; description: string }): Promise<ProductWorkItem> => {
    const newItem: ProductWorkItem = {
      id: `wi-proto-${Date.now()}`,
      title: input.title,
      description: input.description,
      lifecycleState: 'ACTIVE',
      completedAt: null,
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString(),
    };
    inMemoryItems = [newItem, ...inMemoryItems];
    return newItem;
  },

  setLifecycle: async (input: {
    id: string;
    targetLifecycleState: ProductWorkItemLifecycle;
  }): Promise<ProductWorkItem> => {
    const target = inMemoryItems.find((i) => i.id === input.id);
    if (!target) throw new Error(`WorkItem ${input.id} not found in prototype store`);

    const isNowCompleted = input.targetLifecycleState === 'COMPLETED';
    const updated: ProductWorkItem = {
      ...target,
      lifecycleState: input.targetLifecycleState,
      completedAt: isNowCompleted ? new Date().toISOString() : null,
      updatedAt: new Date().toISOString(),
    };

    inMemoryItems = inMemoryItems.map((i) => (i.id === input.id ? updated : i));

    if (inMemoryModels[input.id]) {
      const model = inMemoryModels[input.id];
      const stage = isNowCompleted
        ? 'production'
        : input.targetLifecycleState === 'CANCELLED'
          ? 'queue'
          : 'dev';
      inMemoryModels[input.id] = {
        ...model,
        trunk: {
          ...model.trunk,
          currentStageId: stage,
          label: input.targetLifecycleState,
          progressPercent: isNowCompleted ? 100 : model.trunk.progressPercent,
        },
      };
    }

    return updated;
  },
};
