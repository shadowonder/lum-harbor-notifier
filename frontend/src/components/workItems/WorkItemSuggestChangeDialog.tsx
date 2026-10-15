import { useState } from 'react';
import {
  Alert,
  Box,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Stack,
  TextField,
  Typography,
} from '@mui/material';
import type { ProductWorkItem } from '../../api/workItems/productApi.ts';

export interface SuggestChangeFormValues {
  proposedTitle: string;
  proposedDescription?: string;
  reason?: string;
}

interface WorkItemSuggestChangeDialogProps {
  open: boolean;
  onClose: () => void;
  targetWorkItem: ProductWorkItem;
  onSubmit: (values: SuggestChangeFormValues) => Promise<void>;
  submitting: boolean;
  error?: string | null;
}

interface SuggestFormProps {
  targetWorkItem: ProductWorkItem;
  onSubmit: (values: SuggestChangeFormValues) => Promise<void>;
  onClose: () => void;
  submitting: boolean;
  error?: string | null;
}

function WorkItemSuggestChangeForm({
  targetWorkItem,
  onSubmit,
  onClose,
  submitting,
  error,
}: SuggestFormProps) {
  const [title, setTitle] = useState(targetWorkItem.title);
  const [description, setDescription] = useState(targetWorkItem.description ?? '');
  const [reason, setReason] = useState('');
  const [titleError, setTitleError] = useState<string | null>(null);

  const handleSubmit = async () => {
    const trimmedTitle = title.trim();
    if (!trimmedTitle) {
      setTitleError('Proposed title is required.');
      return;
    }
    setTitleError(null);
    await onSubmit({
      proposedTitle: trimmedTitle,
      proposedDescription: description.trim(),
      reason: reason.trim() || undefined,
    });
  };

  return (
    <>
      <DialogContent>
        <Stack spacing={2} sx={{ pt: 1 }}>
          {error && <Alert severity="error">{error}</Alert>}

          <Box
            sx={{
              p: 1.5,
              borderRadius: 1,
              bgcolor: 'action.hover',
              border: 1,
              borderColor: 'divider',
            }}
          >
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.5 }}>
              DEVELOPMENT-ONLY PROPOSAL PRODUCER
            </Typography>
            <Typography variant="body2" color="text.secondary">
              This test utility creates a real durable Change Proposal through the production M7
              producer boundary. It never edits the authoritative WorkItem directly and is not a
              final product interaction.
            </Typography>
          </Box>

          <TextField
            autoFocus
            required
            label="Proposed Title"
            value={title}
            error={Boolean(titleError)}
            helperText={titleError}
            onChange={(e) => {
              setTitle(e.target.value);
              if (titleError) setTitleError(null);
            }}
            disabled={submitting}
          />

          <TextField
            label="Proposed Description"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            multiline
            minRows={3}
            disabled={submitting}
          />

          <TextField
            label="Rationale / Reason (Optional)"
            placeholder="Why is this change being proposed?"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            multiline
            minRows={2}
            disabled={submitting}
          />
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} disabled={submitting}>
          Cancel
        </Button>
        <Button variant="contained" onClick={handleSubmit} disabled={submitting}>
          {submitting ? 'Creating…' : 'Create Test Proposal'}
        </Button>
      </DialogActions>
    </>
  );
}

export default function WorkItemSuggestChangeDialog({
  open,
  onClose,
  targetWorkItem,
  onSubmit,
  submitting,
  error,
}: WorkItemSuggestChangeDialogProps) {
  return (
    <Dialog open={open} onClose={submitting ? undefined : onClose} fullWidth maxWidth="sm">
      <DialogTitle>Development Proposal Tool</DialogTitle>
      {open && (
        <WorkItemSuggestChangeForm
          targetWorkItem={targetWorkItem}
          onSubmit={onSubmit}
          onClose={onClose}
          submitting={submitting}
          error={error}
        />
      )}
    </Dialog>
  );
}
