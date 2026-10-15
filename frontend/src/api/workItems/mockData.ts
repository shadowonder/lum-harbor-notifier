import type {
  Checkpoint,
  Requirement,
  TrackableItem,
  TrackingDimension,
  TrackingState,
  WorkItem,
  WorkItemProfile,
} from '../../types/workItem';

export const WORK_ITEMS_TODAY = '2026-09-12';

const softwareProfile: WorkItemProfile = {
  label: 'Software Delivery',
  itemsLabel: 'Deliverables',
  trackingLabel: 'Environments',
  progressLabel: 'stories',
  progressCategoryIds: ['user_story'],
  categoryLabels: {
    user_story: 'User Stories',
    critical_fix: 'Critical Fixes',
    enhancement_fix: 'Enhancement Fixes',
    defect: 'Defects',
    configuration: 'Configurations',
  },
};

const learningProfile: WorkItemProfile = {
  label: 'Learning Plan',
  itemsLabel: 'Learning Items',
  trackingLabel: 'Learning Stages',
  progressLabel: 'learning items',
  progressCategoryIds: ['course', 'chapter', 'exercise', 'project', 'exam'],
  categoryLabels: {
    course: 'Courses',
    chapter: 'Chapters',
    exercise: 'Exercises',
    project: 'Projects',
    exam: 'Exams',
    assignment: 'Assignments',
  },
};

const projectProfile: WorkItemProfile = {
  label: 'Project',
  itemsLabel: 'Items',
  trackingLabel: 'Tracking',
  progressLabel: 'items',
  progressCategoryIds: ['work'],
  categoryLabels: { work: 'Work' },
};

const trainingProfile: WorkItemProfile = {
  label: 'Training',
  itemsLabel: 'Modules',
  trackingLabel: 'Tracking',
  progressLabel: 'modules',
  progressCategoryIds: ['module'],
  categoryLabels: { module: 'Modules' },
};

function checkpoint(
  id: string,
  title: string,
  targetAt: string,
  status: Checkpoint['status'],
): Checkpoint {
  return { id, title, targetAt, status };
}

function requirement(
  id: string,
  checkpointId: string | undefined,
  title: string,
  group: string,
  status: Requirement['status'],
  required = true,
): Requirement {
  return { id, checkpointId, title, group, status, required };
}

const releaseCheckpoints: Checkpoint[] = [
  checkpoint('test-ready', 'Test Ready', '2026-09-06', 'completed'),
  checkpoint('stage-ready', 'Stage Ready', '2026-09-09', 'completed'),
  checkpoint('adoption-ready', 'Adoption Ready', '2026-09-12', 'atRisk'),
];

const releaseStories: TrackableItem[] = [
  ['US-101', 'Authentication'],
  ['US-102', 'Account mapping'],
  ['US-103', 'Eligibility rules'],
  ['US-104', 'Enrollment workflow'],
  ['US-105', 'Audit trail'],
  ['US-106', 'Role permissions'],
  ['US-107', 'Notification routing'],
  ['US-108', 'Reporting export'],
  ['US-109', 'Audit enhancement'],
  ['US-110', 'Recovery workflow'],
].map(([reference, title], index) => ({
  id: reference.toLowerCase(),
  reference,
  title,
  category: 'user_story',
  status: index < 8 ? 'complete' : index === 8 ? 'inProgress' : 'queued',
  checkpointIds: ['adoption-ready'],
}));

const releaseItems: TrackableItem[] = [
  ...releaseStories,
  {
    id: 'def-214',
    reference: 'DEF-214',
    title: 'Stage regression',
    category: 'critical_fix',
    status: 'waiting',
    waitingOn: 'Platform Team',
    sourceRefId: 'rally',
    critical: true,
    checkpointIds: ['adoption-ready'],
    evidenceRefs: ['rally-def-214'],
  },
  {
    id: 'fix-219',
    reference: 'FIX-219',
    title: 'Feature enhancement',
    category: 'enhancement_fix',
    status: 'complete',
    checkpointIds: ['adoption-ready'],
  },
  {
    id: 'def-220',
    reference: 'DEF-220',
    title: 'QA retry message mismatch',
    category: 'defect',
    status: 'inProgress',
    checkpointIds: ['adoption-ready'],
  },
  {
    id: 'def-221',
    reference: 'DEF-221',
    title: 'SDET telemetry assertion',
    category: 'defect',
    status: 'deferred',
    checkpointIds: ['adoption-ready'],
  },
  {
    id: 'def-222',
    reference: 'DEF-222',
    title: 'Audit timestamp drift',
    category: 'defect',
    status: 'complete',
    checkpointIds: ['stage-ready'],
  },
  {
    id: 'def-223',
    reference: 'DEF-223',
    title: 'Enrollment retry duplication',
    category: 'defect',
    status: 'complete',
    checkpointIds: ['stage-ready'],
  },
  {
    id: 'def-224',
    reference: 'DEF-224',
    title: 'Reporting export header',
    category: 'defect',
    status: 'complete',
    checkpointIds: ['adoption-ready'],
  },
  {
    id: 'def-225',
    reference: 'DEF-225',
    title: 'Notification fallback copy',
    category: 'defect',
    status: 'complete',
    checkpointIds: ['adoption-ready'],
  },
];

const releaseRequirements: Requirement[] = [
  requirement('flag-test', undefined, 'Test configured', 'Feature Flags', 'ready'),
  requirement('flag-stage', undefined, 'Stage configured', 'Feature Flags', 'ready'),
  requirement(
    'flag-prod',
    undefined,
    'Production Feature Flag configured',
    'Feature Flags',
    'unknown',
  ),
  {
    ...requirement('kafka-schema', undefined, 'Kafka schema registered', 'Kafka', 'ready'),
    evidenceRefs: ['schema-registry-482'],
  },
  requirement(
    'kafka-compat',
    undefined,
    'Kafka backward compatibility verified',
    'Kafka',
    'blocked',
  ),
  requirement('kafka-topic', undefined, 'Kafka topic created', 'Kafka', 'ready'),
  requirement('kafka-acl', undefined, 'Kafka ACL verified', 'Kafka', 'ready'),
  requirement('gcs-test', undefined, 'GCS test bucket configured', 'GCS', 'ready'),
  requirement('gcs-stage', undefined, 'GCS stage bucket configured', 'GCS', 'ready'),
  requirement(
    'gcs-permission',
    undefined,
    'GCS production bucket permission added',
    'GCS',
    'missing',
  ),
  requirement('graphql-gateway', undefined, 'GraphQL gateway deployed', 'GraphQL', 'ready'),
  requirement(
    'enhancement-included',
    undefined,
    'Enhancement fix included',
    'Release Scope',
    'ready',
    false,
  ),
];

const releaseDimensions: TrackingDimension[] = [
  { id: 'test', label: 'Test', order: 0 },
  { id: 'stage', label: 'Stage', order: 1 },
  { id: 'adoption', label: 'Adoption', order: 2 },
  { id: 'prod', label: 'Prod', order: 3 },
];

const releaseTracking: TrackingState[] = releaseItems.flatMap((item) => {
  const states: TrackingState[] = [];
  if (!['us-109', 'us-110'].includes(item.id)) {
    states.push({ dimensionId: 'test', subjectId: item.id, status: 'verified' });
  }
  if (!['us-109', 'us-110'].includes(item.id)) {
    states.push({
      dimensionId: 'stage',
      subjectId: item.id,
      status: item.id === 'def-214' ? 'failed' : item.id === 'def-220' ? 'inProgress' : 'verified',
    });
  }
  if (!['us-107', 'us-108', 'us-109', 'us-110', 'def-214'].includes(item.id)) {
    states.push({ dimensionId: 'adoption', subjectId: item.id, status: 'complete' });
  }
  states.push({ dimensionId: 'prod', subjectId: item.id, status: 'queued' });
  return states;
});

releaseTracking.push(
  { dimensionId: 'test', subjectId: 'flag-prod', status: 'verified' },
  { dimensionId: 'stage', subjectId: 'flag-prod', status: 'verified' },
  { dimensionId: 'adoption', subjectId: 'flag-prod', status: 'complete' },
  { dimensionId: 'prod', subjectId: 'flag-prod', status: 'unknown' },
  { dimensionId: 'test', subjectId: 'kafka-compat', status: 'verified' },
  { dimensionId: 'stage', subjectId: 'kafka-compat', status: 'unknown' },
  { dimensionId: 'adoption', subjectId: 'kafka-compat', status: 'unknown' },
  { dimensionId: 'prod', subjectId: 'kafka-compat', status: 'unknown' },
  { dimensionId: 'test', subjectId: 'gcs-permission', status: 'verified' },
  { dimensionId: 'stage', subjectId: 'gcs-permission', status: 'verified' },
  { dimensionId: 'adoption', subjectId: 'gcs-permission', status: 'verified' },
  { dimensionId: 'prod', subjectId: 'gcs-permission', status: 'blocked' },
);

const learningCheckpoints: Checkpoint[] = [
  checkpoint('course-complete', 'Course Complete', '2026-09-05', 'completed'),
  checkpoint('practice-project', 'Practice Project', '2026-09-15', 'current'),
  checkpoint('mock-exam', 'Mock Exam', '2026-09-22', 'atRisk'),
];

const learningItems: TrackableItem[] = [
  ['course-a', 'DS-101', 'Distributed Systems Foundations', 'course', 'complete'],
  ['chapter-1', 'CH-01', 'Time and ordering', 'chapter', 'complete'],
  ['chapter-2', 'CH-02', 'Replication', 'chapter', 'complete'],
  ['chapter-3', 'CH-03', 'Consensus', 'chapter', 'complete'],
  ['exercise-1', 'EX-01', 'Vector clock exercises', 'exercise', 'complete'],
  ['exercise-2', 'EX-02', 'Raft scenarios', 'exercise', 'complete'],
  ['project-1', 'PRJ-01', 'Replicated key-value store', 'project', 'complete'],
  ['exercise-3', 'EX-03', 'Failure mode practice', 'exercise', 'inProgress'],
  ['project-2', 'PRJ-02', 'Leader election lab', 'project', 'inProgress'],
  ['mock-exam-item', 'EXAM-01', 'Mock exam', 'exam', 'queued'],
  ['review-set', 'ASN-01', 'Extra failure-analysis assignment', 'assignment', 'waiting'],
  ['final-exam-item', 'EXAM-02', 'Final exam', 'exam', 'queued'],
].map(([id, reference, title, category, status]) => ({
  id,
  reference,
  title,
  category,
  status: status as TrackableItem['status'],
  checkpointIds: ['practice-project', 'mock-exam'],
  waitingOn: status === 'waiting' ? 'Mentor review' : undefined,
}));

const learningRequirements: Requirement[] = [
  requirement(
    'course-prerequisite',
    'practice-project',
    'Prerequisite course completed',
    'Prerequisites',
    'ready',
  ),
  requirement(
    'practice-set',
    'practice-project',
    'Failure mode practice set finished',
    'Practice',
    'blocked',
  ),
  requirement(
    'project-repository',
    'practice-project',
    'Project repository prepared',
    'Project',
    'ready',
  ),
  requirement('mentor-review', 'practice-project', 'Mentor review scheduled', 'Review', 'unknown'),
  requirement(
    'reading-notes',
    'practice-project',
    'Reading notes consolidated',
    'Study Support',
    'ready',
    false,
  ),
  requirement('exam-registration', 'mock-exam', 'Mock exam registered', 'Exam', 'missing'),
];

const learningDimensions: TrackingDimension[] = [
  { id: 'learn', label: 'Learn', order: 0 },
  { id: 'practice', label: 'Practice', order: 1 },
  { id: 'review', label: 'Review', order: 2 },
  { id: 'exam', label: 'Exam', order: 3 },
];

const learningTracking: TrackingState[] = learningItems.flatMap(
  (item, index) =>
    [
      {
        dimensionId: 'learn',
        subjectId: item.id,
        status:
          item.id === 'review-set'
            ? 'complete'
            : index < 7
              ? 'complete'
              : index < 9
                ? 'inProgress'
                : 'queued',
      },
      {
        dimensionId: 'practice',
        subjectId: item.id,
        status:
          item.id === 'review-set'
            ? 'complete'
            : index < 6
              ? 'verified'
              : index < 9
                ? 'inProgress'
                : 'queued',
      },
      {
        dimensionId: 'review',
        subjectId: item.id,
        status:
          item.id === 'review-set'
            ? 'complete'
            : index < 4
              ? 'complete'
              : index < 8
                ? 'inProgress'
                : 'queued',
      },
      {
        dimensionId: 'exam',
        subjectId: item.id,
        status:
          item.id === 'mock-exam-item'
            ? 'failed'
            : item.id === 'review-set'
              ? 'waiting'
              : item.category === 'exam'
                ? 'unknown'
                : 'queued',
      },
    ] as TrackingState[],
);

function simpleWorkItem(
  id: string,
  title: string,
  attention: WorkItem['attention'],
  startDate: string,
  targetDate: string,
  withTracking: boolean,
): WorkItem {
  const items: TrackableItem[] = Array.from({ length: 5 }, (_, index) => ({
    id: `${id}-${index + 1}`,
    title: `Work item ${index + 1}`,
    category: 'work',
    status: index < 3 ? 'complete' : 'inProgress',
  }));
  const dimensions: TrackingDimension[] = withTracking
    ? [{ id: 'delivery', label: 'Delivery', order: 0 }]
    : [];
  return {
    id,
    title,
    labels: ['Project'],
    profile: projectProfile,
    status: 'active',
    currentState: 'In progress',
    attention,
    targetAt: targetDate,
    startedAt: startDate,
    updatedAt: '2026-09-11T15:20:00-05:00',
    checkpoints: [],
    items,
    requirements: [
      requirement(`${id}-gate-1`, undefined, 'Scope confirmed', 'Readiness', 'ready'),
      requirement(`${id}-gate-2`, undefined, 'Stakeholder review', 'Readiness', 'unknown'),
    ],
    relations: [],
    trackingDimensions: dimensions,
    trackingStates: dimensions.length
      ? items.map((item) => ({
          dimensionId: 'delivery',
          subjectId: item.id,
          status: item.status === 'complete' ? 'complete' : 'inProgress',
        }))
      : [],
    nextActions: ['Confirm the next review', 'Complete the remaining work'],
    relatedTodoCount: 2,
    relatedEventCount: 1,
    activity: [],
    milestones: [
      {
        id: `${id}-progress`,
        label: 'Stakeholder review prepared',
        occurredAt: '2026-09-11T15:20:00-05:00',
        kind: 'progress',
      },
    ],
  };
}

const completedReleaseItems: TrackableItem[] = [
  ...Array.from({ length: 10 }, (_, index) => ({
    id: `identity-story-${index + 1}`,
    reference: `ID-${index + 1}`,
    title: `Identity delivery story ${index + 1}`,
    category: 'user_story',
    status: 'complete' as const,
  })),
  ...Array.from({ length: 7 }, (_, index) => ({
    id: `identity-defect-${index + 1}`,
    reference: `ID-DEF-${index + 1}`,
    title: `Resolved identity defect ${index + 1}`,
    category: 'defect',
    status: 'complete' as const,
  })),
];

const completedReleaseRequirements: Requirement[] = [
  ...Array.from({ length: 3 }, (_, index) =>
    requirement(
      `identity-flag-${index + 1}`,
      undefined,
      `Feature flag ${index + 1} verified`,
      'Feature Flags',
      'ready',
    ),
  ),
  ...Array.from({ length: 3 }, (_, index) =>
    requirement(
      `identity-bucket-${index + 1}`,
      undefined,
      `GCS bucket ${index + 1} verified`,
      'GCS',
      'ready',
    ),
  ),
];

const completedReleaseDimensions: TrackingDimension[] = [
  { id: 'identity-test', label: 'Test', order: 0 },
  { id: 'identity-stage', label: 'Stage', order: 1 },
  { id: 'identity-prod', label: 'Production', order: 2 },
];

const completedRelease: WorkItem = {
  id: 'customer-identity-release',
  title: 'Customer Identity Release',
  labels: ['Release', 'Identity', 'Production'],
  profile: softwareProfile,
  status: 'complete',
  currentState: 'Completed',
  attention: 'low',
  targetAt: '2026-09-09',
  startedAt: '2026-08-13',
  completedAt: '2026-09-09',
  finalOutcome: 'Production deployed Sep 8',
  updatedAt: '2026-09-09T16:40:00-05:00',
  checkpoints: [
    checkpoint('identity-test-ready', 'Test Ready', '2026-08-28', 'completed'),
    checkpoint('identity-stage-ready', 'Stage Ready', '2026-09-03', 'completed'),
    checkpoint('identity-prod-ready', 'Production Ready', '2026-09-08', 'completed'),
  ],
  items: completedReleaseItems,
  requirements: completedReleaseRequirements,
  relations: completedReleaseRequirements.map((entry) => ({
    id: `identity-rel-${entry.id}`,
    sourceId: entry.id,
    targetId: 'customer-identity-release',
    type: 'requires' as const,
  })),
  trackingDimensions: completedReleaseDimensions,
  trackingStates: completedReleaseItems.flatMap((item) =>
    completedReleaseDimensions.map((dimension) => ({
      dimensionId: dimension.id,
      subjectId: item.id,
      status: 'verified' as const,
    })),
  ),
  nextActions: [],
  relatedTodoCount: 0,
  relatedEventCount: 1,
  milestones: [
    {
      id: 'identity-m1',
      label: 'Test deployment completed',
      occurredAt: '2026-08-28T15:10:00-05:00',
      kind: 'deployment',
    },
    {
      id: 'identity-m2',
      label: 'Stage deployment completed',
      occurredAt: '2026-09-03T14:20:00-05:00',
      kind: 'deployment',
    },
    {
      id: 'identity-m3',
      label: 'Production deployment completed',
      occurredAt: '2026-09-08T17:05:00-05:00',
      kind: 'deployment',
    },
    {
      id: 'identity-m4',
      label: 'Release completed',
      occurredAt: '2026-09-09T16:40:00-05:00',
      kind: 'completion',
    },
  ],
  activity: [
    { id: 'identity-a1', label: 'Release completed', occurredAt: 'Sep 9, 4:40 PM' },
    { id: 'identity-a2', label: 'Production deployment completed', occurredAt: 'Sep 8, 5:05 PM' },
    { id: 'identity-a3', label: 'Stage deployment completed', occurredAt: 'Sep 3, 2:20 PM' },
    { id: 'identity-a4', label: 'Test deployment completed', occurredAt: 'Aug 28, 3:10 PM' },
    { id: 'identity-a5', label: 'Release started', occurredAt: 'Aug 13' },
  ],
};

const completedCertification: WorkItem = {
  id: 'cloud-architecture-certification',
  title: 'Cloud Architecture Certification',
  labels: ['Learning', 'Certification', 'Personal'],
  profile: learningProfile,
  status: 'complete',
  currentState: 'Completed',
  attention: 'low',
  targetAt: '2026-09-02',
  startedAt: '2026-08-05',
  completedAt: '2026-09-02',
  finalOutcome: 'Final score 96%',
  updatedAt: '2026-09-02T13:25:00-05:00',
  checkpoints: [checkpoint('cloud-cert-final', 'Final Assessment', '2026-09-02', 'completed')],
  items: [
    { id: 'cloud-course', title: 'Architecture course', category: 'course', status: 'complete' },
    { id: 'cloud-lab', title: 'Resilience lab', category: 'project', status: 'complete' },
    { id: 'cloud-quiz', title: 'Final quiz', category: 'exam', status: 'complete' },
    {
      id: 'cloud-assignment',
      title: 'Architecture assignment',
      category: 'assignment',
      status: 'complete',
    },
  ],
  requirements: [
    requirement(
      'cloud-review',
      'cloud-cert-final',
      'Instructor review completed',
      'Review',
      'ready',
    ),
  ],
  relations: [
    {
      id: 'cloud-review-rel',
      sourceId: 'cloud-review',
      targetId: 'cloud-cert-final',
      type: 'requires',
    },
  ],
  trackingDimensions: [{ id: 'cloud-review-stage', label: 'Review', order: 0 }],
  trackingStates: ['cloud-course', 'cloud-lab', 'cloud-quiz', 'cloud-assignment'].map(
    (subjectId) => ({ dimensionId: 'cloud-review-stage', subjectId, status: 'verified' }),
  ),
  nextActions: [],
  relatedTodoCount: 0,
  relatedEventCount: 0,
  milestones: [
    {
      id: 'cloud-m1',
      label: 'Assignment submitted',
      occurredAt: '2026-08-25T18:00:00-05:00',
      kind: 'submission',
    },
    {
      id: 'cloud-m2',
      label: 'Lab uploaded',
      occurredAt: '2026-08-27T19:30:00-05:00',
      kind: 'submission',
    },
    {
      id: 'cloud-m3',
      label: 'Instructor review completed',
      occurredAt: '2026-08-30T10:15:00-05:00',
      kind: 'review',
    },
    {
      id: 'cloud-m4',
      label: 'Final quiz passed',
      occurredAt: '2026-09-01T12:40:00-05:00',
      kind: 'progress',
    },
    {
      id: 'cloud-m5',
      label: 'Grade received · 96%',
      occurredAt: '2026-09-02T13:25:00-05:00',
      kind: 'grading',
    },
  ],
  activity: [
    { id: 'cloud-a1', label: 'Grade received · 96%', occurredAt: 'Sep 2, 1:25 PM' },
    { id: 'cloud-a2', label: 'Final quiz passed', occurredAt: 'Sep 1, 12:40 PM' },
    { id: 'cloud-a3', label: 'Instructor review completed', occurredAt: 'Aug 30, 10:15 AM' },
    { id: 'cloud-a4', label: 'Lab uploaded', occurredAt: 'Aug 27, 7:30 PM' },
    { id: 'cloud-a5', label: 'Assignment submitted', occurredAt: 'Aug 25, 6:00 PM' },
    { id: 'cloud-a6', label: 'Certification work started', occurredAt: 'Aug 5' },
  ],
};

const completedAccessReview: WorkItem = {
  ...simpleWorkItem(
    'q3-access-review',
    'Q3 Access Review',
    'low',
    '2026-08-18',
    '2026-08-29',
    false,
  ),
  status: 'complete',
  currentState: 'Completed',
  attention: 'low',
  startedAt: '2026-08-18',
  completedAt: '2026-08-29',
  finalOutcome: 'All access exceptions closed',
  updatedAt: '2026-08-29T11:30:00-05:00',
  items: Array.from({ length: 5 }, (_, index) => ({
    id: `access-review-${index + 1}`,
    title: `Access group ${index + 1}`,
    category: 'work',
    status: 'complete',
  })),
  requirements: [
    requirement('access-approval', undefined, 'Owner approval recorded', 'Readiness', 'ready'),
  ],
  nextActions: [],
  milestones: [
    {
      id: 'access-m1',
      label: 'Owner approvals received',
      occurredAt: '2026-08-28T15:00:00-05:00',
      kind: 'review',
    },
    {
      id: 'access-m2',
      label: 'Access review completed',
      occurredAt: '2026-08-29T11:30:00-05:00',
      kind: 'completion',
    },
  ],
  activity: [
    { id: 'access-a1', label: 'Access review completed', occurredAt: 'Aug 29, 11:30 AM' },
    { id: 'access-a2', label: 'Owner approvals received', occurredAt: 'Aug 28, 3:00 PM' },
    { id: 'access-a3', label: 'Access review started', occurredAt: 'Aug 18' },
  ],
};

const completedSecurityTraining: WorkItem = {
  id: 'security-fundamentals-training',
  title: 'Security Fundamentals Training',
  labels: ['Training', 'Compliance', 'Company'],
  profile: trainingProfile,
  status: 'complete',
  currentState: 'Completed',
  attention: 'low',
  targetAt: '2026-08-24',
  startedAt: '2026-08-17',
  completedAt: '2026-08-24',
  finalOutcome: 'All five modules passed',
  updatedAt: '2026-08-24T08:30:00-05:00',
  checkpoints: [],
  items: Array.from({ length: 5 }, (_, index) => ({
    id: `security-fundamentals-${index + 1}`,
    title: `Security module ${index + 1}`,
    category: 'module',
    status: 'complete',
  })),
  requirements: [],
  relations: [],
  trackingDimensions: [],
  trackingStates: [],
  nextActions: [],
  relatedTodoCount: 0,
  relatedEventCount: 0,
  milestones: [
    {
      id: 'security-fundamentals-m1',
      label: 'Final quiz passed',
      occurredAt: '2026-08-23T16:20:00-05:00',
      kind: 'progress',
    },
    {
      id: 'security-fundamentals-m2',
      label: 'Training completed',
      occurredAt: '2026-08-24T08:30:00-05:00',
      kind: 'completion',
    },
  ],
  activity: [
    { id: 'security-fundamentals-a1', label: 'Training completed', occurredAt: 'Aug 24, 8:30 AM' },
    { id: 'security-fundamentals-a2', label: 'Final quiz passed', occurredAt: 'Aug 23, 4:20 PM' },
  ],
};

export const workItems: WorkItem[] = [
  {
    id: 'production-release',
    title: 'Production Release',
    labels: ['Release', 'Production', 'Rally'],
    profile: softwareProfile,
    status: 'active',
    currentState: 'Release validation',
    attention: 'high',
    targetAt: '2026-09-15',
    forecastAt: '2026-09-17',
    startedAt: '2026-08-25',
    updatedAt: '2026-09-12T09:10:00-05:00',
    checkpoints: releaseCheckpoints,
    activeCheckpointId: 'adoption-ready',
    items: releaseItems,
    requirements: releaseRequirements,
    relations: [
      ...releaseRequirements
        .filter((item) => item.required)
        .map((item) => ({
          id: `rel-${item.id}-production`,
          sourceId: item.id,
          targetId: 'production-release',
          type: 'requires' as const,
        })),
      {
        id: 'rel-schema-compat',
        sourceId: 'kafka-schema',
        targetId: 'kafka-compat',
        type: 'requires',
      },
      {
        id: 'rel-def-release',
        sourceId: 'def-214',
        targetId: 'production-release',
        type: 'blocks',
      },
    ],
    trackingDimensions: releaseDimensions,
    trackingStates: releaseTracking,
    sources: [
      {
        id: 'rally',
        source: 'Rally',
        externalId: 'REL-482',
        label: 'Release scope',
        hrefLabel: 'Open in Rally',
        url: 'https://rally1.rallydev.com/',
        updatedAt: 'Sep 12, 9:10 AM',
      },
      {
        id: 'platform',
        source: 'Platform',
        externalId: 'CHG-781',
        label: 'Production controls',
        hrefLabel: 'Open source record',
        updatedAt: 'Sep 12, 8:42 AM',
      },
    ],
    evidence: [
      {
        id: 'rally-def-214',
        label: 'Stage regression result',
        sourceRefId: 'rally',
        capturedAt: 'Sep 11, 4:42 PM',
      },
      {
        id: 'schema-registry-482',
        label: 'Schema compatibility result',
        sourceRefId: 'platform',
        capturedAt: 'Sep 11, 11:08 AM',
      },
    ],
    nextActions: [
      'Verify Kafka backward compatibility',
      'Confirm the production Feature Flag',
      'Resolve DEF-214 before release approval',
    ],
    relatedTodoCount: 3,
    relatedEventCount: 2,
    activity: [
      {
        id: 'release-a1',
        label: 'Stage validation failed for DEF-214',
        occurredAt: 'Yesterday, 16:42',
        entityIds: ['def-214'],
      },
      {
        id: 'release-a2',
        label: 'Kafka schema registered',
        occurredAt: 'Yesterday, 11:08',
        entityIds: ['kafka-schema'],
      },
    ],
    milestones: [
      {
        id: 'release-m1',
        label: 'Test deployment completed',
        occurredAt: '2026-09-04T14:10:00-05:00',
        kind: 'deployment',
      },
      {
        id: 'release-m2',
        label: 'Six stories passed validation',
        occurredAt: '2026-09-10T16:20:00-05:00',
        kind: 'progress',
      },
      {
        id: 'release-m3',
        label: 'Stage deployment completed',
        occurredAt: '2026-09-11T18:05:00-05:00',
        kind: 'deployment',
      },
    ],
  },
  {
    id: 'distributed-systems-learning',
    title: 'Distributed Systems Learning Plan',
    labels: ['Learning', 'Certification', 'Personal'],
    profile: learningProfile,
    status: 'active',
    currentState: 'Practice and application',
    attention: 'attention',
    targetAt: '2026-09-29',
    startedAt: '2026-09-01',
    updatedAt: '2026-09-11T20:05:00-05:00',
    checkpoints: learningCheckpoints,
    activeCheckpointId: 'practice-project',
    items: learningItems,
    requirements: learningRequirements,
    relations: [
      {
        id: 'rel-practice-milestone',
        sourceId: 'practice-set',
        targetId: 'practice-project',
        type: 'requires',
      },
      {
        id: 'rel-review-milestone',
        sourceId: 'mentor-review',
        targetId: 'practice-project',
        type: 'requires',
      },
      {
        id: 'rel-exercise-practice',
        sourceId: 'exercise-3',
        targetId: 'practice-set',
        type: 'requires',
      },
      {
        id: 'rel-project-practice',
        sourceId: 'project-2',
        targetId: 'practice-project',
        type: 'requires',
      },
    ],
    trackingDimensions: learningDimensions,
    trackingStates: learningTracking,
    sources: [
      {
        id: 'learning-portal',
        source: 'Learning Portal',
        externalId: 'PLAN-DS-26',
        label: 'Course plan',
        hrefLabel: 'Open course plan',
        updatedAt: 'Sep 11, 8:05 PM',
      },
    ],
    nextActions: [
      'Finish the failure mode practice set',
      'Complete the leader election lab',
      'Confirm the mentor review',
    ],
    relatedTodoCount: 4,
    relatedEventCount: 2,
    activity: [
      {
        id: 'learn-a1',
        label: 'Replication project completed',
        occurredAt: 'Yesterday, 19:10',
        entityIds: ['project-1'],
      },
    ],
    milestones: [
      {
        id: 'learn-m1',
        label: 'Assignment submitted',
        occurredAt: '2026-09-08T19:10:00-05:00',
        kind: 'submission',
      },
      {
        id: 'learn-m2',
        label: 'Review 1 completed',
        occurredAt: '2026-09-12T09:00:00-05:00',
        kind: 'review',
      },
    ],
  },
  simpleWorkItem('lob-onboarding', 'LOB Onboarding', 'normal', '2026-09-08', '2026-09-18', true),
  {
    id: 'company-security-training',
    title: 'Company Security Training',
    labels: ['Training', 'Deadline', 'Company'],
    profile: trainingProfile,
    status: 'active',
    currentState: 'Three of five modules complete',
    attention: 'normal',
    targetAt: '2026-10-15',
    startedAt: '2026-09-10',
    updatedAt: '2026-09-10T08:30:00-05:00',
    checkpoints: [],
    items: Array.from({ length: 5 }, (_, index) => ({
      id: `security-module-${index + 1}`,
      title: `Security module ${index + 1}`,
      category: 'module',
      status: index < 3 ? 'complete' : 'queued',
    })),
    requirements: [],
    relations: [],
    trackingDimensions: [],
    trackingStates: [],
    nextActions: ['Complete the data handling module'],
    relatedTodoCount: 1,
    relatedEventCount: 0,
    milestones: [
      {
        id: 'security-m1',
        label: 'Training assigned',
        occurredAt: '2026-09-10T08:30:00-05:00',
        kind: 'progress',
      },
    ],
    activity: [{ id: 'security-a1', label: 'Training assigned', occurredAt: 'Sep 10, 8:30 AM' }],
  },
  completedRelease,
  completedCertification,
  completedAccessReview,
  completedSecurityTraining,
];

export function findWorkItem(id: string | undefined) {
  return workItems.find((item) => item.id === id);
}

export type { WorkItem } from '../../types/workItem';
