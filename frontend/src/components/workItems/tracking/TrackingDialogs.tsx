import { useState } from 'react';
import AddRounded from '@mui/icons-material/AddRounded';
import DeleteOutlineRounded from '@mui/icons-material/DeleteOutlineRounded';
import {
  Alert,
  Button,
  Checkbox,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  FormControlLabel,
  IconButton,
  MenuItem,
  Stack,
  TextField,
  Typography,
} from '@mui/material';
import type {
  CreateTrackedItemInput,
  CreateWorkflowInput,
  ExpectedWorkItemLifecycle,
  MonitorKind,
  TrackedItem,
  TrackingWorkflowDefinition,
} from '../../../api/tracking/trackingProductApi.ts';
import type { TrackingMutationOutcome } from '../../../tracking/trackingMutation.ts';
import {
  monitorKindLabel,
  type MonitorFormValues,
} from '../../../tracking/trackingPresentation.ts';

type Submit<T> = (values: T) => Promise<TrackingMutationOutcome>;

/** A dialog closes when the change committed, even if the follow-up refresh failed. */
function committed(outcome: TrackingMutationOutcome) {
  return outcome.status !== 'mutationFailed';
}

export function CreateWorkflowDialog({
  open,
  onClose,
  onSubmit,
}: {
  open: boolean;
  onClose: () => void;
  onSubmit: Submit<CreateWorkflowInput>;
}) {
  const [name, setName] = useState('');
  const [version, setVersion] = useState('1');
  const [steps, setSteps] = useState([{ key: '', title: '', terminal: false }]);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const reset = () => {
    setName('');
    setVersion('1');
    setSteps([{ key: '', title: '', terminal: false }]);
    setError(null);
  };
  const close = () => {
    if (submitting) return;
    reset();
    onClose();
  };
  const update = (index: number, patch: Partial<(typeof steps)[number]>) =>
    setSteps((current) => current.map((s, i) => (i === index ? { ...s, ...patch } : s)));

  const submit = async () => {
    const parsedVersion = Number(version);
    if (!name.trim()) return setError('A workflow name is required.');
    if (!Number.isInteger(parsedVersion) || parsedVersion < 1)
      return setError('Version must be a positive whole number.');
    if (steps.some((s) => !s.key.trim() || !s.title.trim()))
      return setError('Every step needs a key and a title.');
    setError(null);
    setSubmitting(true);
    const outcome = await onSubmit({
      name: name.trim(),
      version: parsedVersion,
      steps: steps.map((s) => ({ key: s.key.trim(), title: s.title.trim(), terminal: s.terminal })),
    });
    setSubmitting(false);
    if (committed(outcome)) {
      reset();
      onClose();
    } else setError(outcome.message);
  };

  return (
    <Dialog open={open} onClose={close} fullWidth maxWidth="sm">
      <DialogTitle>Create Workflow</DialogTitle>
      <DialogContent>
        <Stack spacing={2} sx={{ pt: 1 }}>
          {error && <Alert severity="error">{error}</Alert>}
          <Stack direction="row" spacing={1.5}>
            <TextField
              label="Name"
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
              disabled={submitting}
              sx={{ flex: 1 }}
            />
            <TextField
              label="Version"
              value={version}
              onChange={(e) => setVersion(e.target.value)}
              disabled={submitting}
              sx={{ width: 110 }}
              slotProps={{ htmlInput: { inputMode: 'numeric' } }}
            />
          </Stack>
          <Typography variant="caption" color="text.secondary">
            Steps run in the order listed. A workflow cannot be edited after it is created; create a
            new version instead.
          </Typography>
          {steps.map((step, index) => (
            <Stack key={index} direction="row" spacing={1} sx={{ alignItems: 'center' }}>
              <TextField
                size="small"
                label="Key"
                value={step.key}
                onChange={(e) => update(index, { key: e.target.value })}
                disabled={submitting}
                sx={{ width: 130 }}
              />
              <TextField
                size="small"
                label="Title"
                value={step.title}
                onChange={(e) => update(index, { title: e.target.value })}
                disabled={submitting}
                sx={{ flex: 1 }}
              />
              <FormControlLabel
                control={
                  <Checkbox
                    size="small"
                    checked={step.terminal}
                    onChange={(e) => update(index, { terminal: e.target.checked })}
                    disabled={submitting}
                  />
                }
                label="Final"
              />
              <IconButton
                aria-label="Remove step"
                size="small"
                disabled={submitting || steps.length === 1}
                onClick={() => setSteps((current) => current.filter((_, i) => i !== index))}
              >
                <DeleteOutlineRounded fontSize="small" />
              </IconButton>
            </Stack>
          ))}
          <Button
            size="small"
            startIcon={<AddRounded />}
            disabled={submitting}
            onClick={() =>
              setSteps((current) => [...current, { key: '', title: '', terminal: false }])
            }
            sx={{ alignSelf: 'flex-start' }}
          >
            Add step
          </Button>
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={close} disabled={submitting}>
          Cancel
        </Button>
        <Button variant="contained" onClick={submit} disabled={submitting}>
          {submitting ? 'Creating…' : 'Create Workflow'}
        </Button>
      </DialogActions>
    </Dialog>
  );
}

export function AddTrackedItemDialog({
  open,
  workItemId,
  workflows,
  workflowsError,
  existingItems,
  onClose,
  onSubmit,
  onCreateWorkflow,
}: {
  open: boolean;
  workItemId: string;
  workflows: TrackingWorkflowDefinition[];
  workflowsError: string | null;
  existingItems: TrackedItem[];
  onClose: () => void;
  onSubmit: Submit<CreateTrackedItemInput>;
  onCreateWorkflow: () => void;
}) {
  const [title, setTitle] = useState('');
  const [itemType, setItemType] = useState('');
  const [parentId, setParentId] = useState('');
  const [workflowId, setWorkflowId] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const close = () => {
    if (submitting) return;
    setTitle('');
    setItemType('');
    setParentId('');
    setWorkflowId('');
    setError(null);
    onClose();
  };

  const submit = async () => {
    if (!title.trim()) return setError('A title is required.');
    setError(null);
    setSubmitting(true);
    const outcome = await onSubmit({
      workItemId,
      title: title.trim(),
      ...(itemType.trim() ? { itemType: itemType.trim() } : {}),
      ...(parentId ? { parentItemId: parentId } : {}),
      ...(workflowId ? { workflowId } : {}),
    });
    setSubmitting(false);
    if (committed(outcome)) {
      setTitle('');
      setItemType('');
      setParentId('');
      setWorkflowId('');
      onClose();
    } else setError(outcome.message);
  };

  return (
    <Dialog open={open} onClose={close} fullWidth maxWidth="sm">
      <DialogTitle>Add Tracked Item</DialogTitle>
      <DialogContent>
        <Stack spacing={2} sx={{ pt: 1 }}>
          {error && <Alert severity="error">{error}</Alert>}
          {workflowsError && (
            <Alert severity="warning">Workflows could not be loaded: {workflowsError}</Alert>
          )}
          <TextField
            autoFocus
            required
            label="Title"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            disabled={submitting}
          />
          <TextField
            label="Type (optional)"
            value={itemType}
            onChange={(e) => setItemType(e.target.value)}
            disabled={submitting}
          />
          <TextField
            select
            label="Parent (optional)"
            value={parentId}
            onChange={(e) => setParentId(e.target.value)}
            disabled={submitting}
          >
            <MenuItem value="">None</MenuItem>
            {existingItems.map((candidate) => (
              <MenuItem key={candidate.id} value={candidate.id}>
                {candidate.title}
              </MenuItem>
            ))}
          </TextField>
          <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
            <TextField
              select
              label="Workflow (optional)"
              value={workflowId}
              onChange={(e) => setWorkflowId(e.target.value)}
              disabled={submitting}
              sx={{ flex: 1 }}
            >
              <MenuItem value="">No workflow</MenuItem>
              {workflows.map((workflow) => (
                <MenuItem key={workflow.id} value={workflow.id}>
                  {workflow.name} v{workflow.version} · {workflow.steps.length} steps
                </MenuItem>
              ))}
            </TextField>
            <Button size="small" onClick={onCreateWorkflow} disabled={submitting}>
              New workflow…
            </Button>
          </Stack>
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={close} disabled={submitting}>
          Cancel
        </Button>
        <Button variant="contained" onClick={submit} disabled={submitting}>
          {submitting ? 'Adding…' : 'Add'}
        </Button>
      </DialogActions>
    </Dialog>
  );
}

const targetKinds: MonitorKind[] = ['FINAL_TARGET', 'CHECKPOINT', 'DEADLINE', 'REVIEW'];
const lifecycles: ExpectedWorkItemLifecycle[] = ['COMPLETED', 'ACTIVE', 'PAUSED', 'CANCELLED'];

export function SetTargetDialog({
  open,
  onClose,
  onSubmit,
}: {
  open: boolean;
  onClose: () => void;
  onSubmit: Submit<MonitorFormValues>;
}) {
  const [kind, setKind] = useState<MonitorKind>('FINAL_TARGET');
  const [title, setTitle] = useState('');
  const [dueLocal, setDueLocal] = useState('');
  const [lifecycle, setLifecycle] = useState<ExpectedWorkItemLifecycle>('COMPLETED');
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const close = () => {
    if (submitting) return;
    setTitle('');
    setDueLocal('');
    setError(null);
    onClose();
  };

  const submit = async () => {
    if (!title.trim()) return setError('A target title is required.');
    if (!dueLocal) return setError('Choose a due date and time.');
    setError(null);
    setSubmitting(true);
    let outcome: TrackingMutationOutcome;
    try {
      outcome = await onSubmit({ kind, title, dueLocal, expectedLifecycle: lifecycle });
    } catch (failure) {
      outcome = {
        status: 'mutationFailed',
        message: failure instanceof Error ? failure.message : String(failure),
      };
    }
    setSubmitting(false);
    if (committed(outcome)) {
      setTitle('');
      setDueLocal('');
      onClose();
    } else setError(outcome.message);
  };

  return (
    <Dialog open={open} onClose={close} fullWidth maxWidth="sm">
      <DialogTitle>Set Target</DialogTitle>
      <DialogContent>
        <Stack spacing={2} sx={{ pt: 1 }}>
          {error && <Alert severity="error">{error}</Alert>}
          <TextField
            select
            label="Kind"
            value={kind}
            onChange={(e) => setKind(e.target.value as MonitorKind)}
            disabled={submitting}
          >
            {targetKinds.map((k) => (
              <MenuItem key={k} value={k}>
                {monitorKindLabel(k)}
              </MenuItem>
            ))}
          </TextField>
          <TextField
            autoFocus
            required
            label="Title"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            disabled={submitting}
          />
          <TextField
            select
            label="This Work Item should be"
            value={lifecycle}
            onChange={(e) => setLifecycle(e.target.value as ExpectedWorkItemLifecycle)}
            disabled={submitting}
          >
            {lifecycles.map((l) => (
              <MenuItem key={l} value={l}>
                {l.charAt(0) + l.slice(1).toLowerCase()}
              </MenuItem>
            ))}
          </TextField>
          <TextField
            required
            type="datetime-local"
            label="By"
            value={dueLocal}
            onChange={(e) => setDueLocal(e.target.value)}
            disabled={submitting}
            slotProps={{ inputLabel: { shrink: true } }}
          />
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={close} disabled={submitting}>
          Cancel
        </Button>
        <Button variant="contained" onClick={submit} disabled={submitting}>
          {submitting ? 'Saving…' : 'Set Target'}
        </Button>
      </DialogActions>
    </Dialog>
  );
}
