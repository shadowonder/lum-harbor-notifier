import { useState } from 'react';
import {
  Alert,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Stack,
  TextField,
} from '@mui/material';

interface WorkItemCreateDialogProps {
  open: boolean;
  onClose: () => void;
  onCreate: (title: string, description: string) => Promise<void>;
  submitting: boolean;
  error: string | null;
}

export default function WorkItemCreateDialog({
  open,
  onClose,
  onCreate,
  submitting,
  error,
}: WorkItemCreateDialogProps) {
  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const [titleError, setTitleError] = useState<string | null>(null);

  const handleClose = () => {
    if (submitting) return;
    setTitle('');
    setDescription('');
    setTitleError(null);
    onClose();
  };

  const handleCreate = async () => {
    if (!title.trim()) {
      setTitleError('Title is required.');
      return;
    }
    setTitleError(null);
    await onCreate(title.trim(), description.trim());
    setTitle('');
    setDescription('');
  };

  return (
    <Dialog open={open} onClose={submitting ? undefined : handleClose} fullWidth maxWidth="sm">
      <DialogTitle>Create Work Item</DialogTitle>
      <DialogContent>
        <Stack spacing={2} sx={{ pt: 1 }}>
          {error && <Alert severity="error">{error}</Alert>}
          <TextField
            autoFocus
            required
            label="Title"
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
            label="Description"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            multiline
            minRows={3}
            disabled={submitting}
          />
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={handleClose} disabled={submitting}>
          Cancel
        </Button>
        <Button variant="contained" onClick={handleCreate} disabled={submitting}>
          {submitting ? 'Creating…' : 'Create'}
        </Button>
      </DialogActions>
    </Dialog>
  );
}

