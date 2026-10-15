import ArrowBackRounded from '@mui/icons-material/ArrowBackRounded';
import CloseRounded from '@mui/icons-material/CloseRounded';
import CodeRounded from '@mui/icons-material/CodeRounded';
import {
  Alert,
  Box,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Divider,
  Drawer,
  FormControl,
  FormControlLabel,
  IconButton,
  InputLabel,
  MenuItem,
  Paper,
  Select,
  Stack,
  Switch,
  TextField,
  Typography,
} from '@mui/material';
import { useState } from 'react';
import type { ReactNode } from 'react';
import type {
  HealthTone,
  SettingsConfigDocument,
  SettingsConfigValue,
  SettingsFieldDescriptor,
} from './model';
import { parseRawConfiguration } from './model';

const toneColor: Record<HealthTone, string> = {
  healthy: 'success.main',
  attention: 'warning.main',
  neutral: 'text.secondary',
  inactive: 'text.disabled',
};

export function SettingsStatus({ label, tone = 'neutral' }: { label: string; tone?: HealthTone }) {
  return (
    <Stack direction="row" spacing={0.75} sx={{ alignItems: 'center', minWidth: 0 }}>
      <Box
        aria-hidden="true"
        sx={{
          width: 6,
          height: 6,
          borderRadius: '50%',
          bgcolor: toneColor[tone],
          flex: '0 0 auto',
        }}
      />
      <Typography variant="caption" color="text.secondary" noWrap>
        {label}
      </Typography>
    </Stack>
  );
}

export function SettingsPageHeader({
  title,
  description,
  onBack,
  status,
}: {
  title: string;
  description: string;
  onBack: () => void;
  status?: ReactNode;
}) {
  return (
    <Stack spacing={1.4} sx={{ mb: 3 }}>
      <Button
        onClick={onBack}
        startIcon={<ArrowBackRounded />}
        sx={{ alignSelf: 'flex-start', color: 'text.secondary', px: 0.5 }}
      >
        Settings
      </Button>
      <Stack
        direction="row"
        spacing={2}
        sx={{ justifyContent: 'space-between', alignItems: 'flex-start' }}
      >
        <Box>
          <Typography component="h1" variant="h1">
            {title}
          </Typography>
          <Typography color="text.secondary" sx={{ mt: 0.75, maxWidth: 720 }}>
            {description}
          </Typography>
        </Box>
        {status}
      </Stack>
    </Stack>
  );
}

export function SettingsSection({
  id,
  title,
  description,
  action,
  highlighted,
  children,
}: {
  id: string;
  title: string;
  description?: string;
  action?: ReactNode;
  highlighted?: boolean;
  children: ReactNode;
}) {
  return (
    <Paper
      id={id}
      tabIndex={-1}
      variant="outlined"
      sx={{
        p: { xs: 2, md: 2.5 },
        scrollMarginTop: 24,
        outline: highlighted ? '2px solid' : 'none',
        outlineColor: 'primary.main',
        transition: 'outline-color 180ms ease',
      }}
    >
      <Stack
        direction="row"
        spacing={2}
        sx={{ justifyContent: 'space-between', alignItems: 'flex-start' }}
      >
        <Box>
          <Typography component="h2" variant="h2">
            {title}
          </Typography>
          {description && (
            <Typography color="text.secondary" sx={{ mt: 0.5 }}>
              {description}
            </Typography>
          )}
        </Box>
        {action}
      </Stack>
      <Box sx={{ mt: 2 }}>{children}</Box>
    </Paper>
  );
}

export function SettingsDetailDrawer({
  open,
  title,
  subtitle,
  onClose,
  children,
}: {
  open: boolean;
  title: string;
  subtitle?: string;
  onClose: () => void;
  children: ReactNode;
}) {
  return (
    <Drawer
      anchor="right"
      open={open}
      onClose={onClose}
      slotProps={{
        paper: {
          sx: { width: 520, maxWidth: '92vw', bgcolor: 'background.paper' },
        },
      }}
    >
      <Stack sx={{ height: '100%', minHeight: 0 }}>
        <Stack direction="row" spacing={1.5} sx={{ p: 2.5, alignItems: 'flex-start' }}>
          <Box sx={{ minWidth: 0, flex: 1 }}>
            <Typography component="h2" variant="h1" sx={{ fontSize: '1.2rem' }}>
              {title}
            </Typography>
            {subtitle && (
              <Typography color="text.secondary" sx={{ mt: 0.5 }}>
                {subtitle}
              </Typography>
            )}
          </Box>
          <IconButton onClick={onClose} aria-label="Close details">
            <CloseRounded />
          </IconButton>
        </Stack>
        <Divider />
        <Box sx={{ p: 2.5, overflowY: 'auto', minHeight: 0 }}>{children}</Box>
      </Stack>
    </Drawer>
  );
}

function fieldValue(
  field: SettingsFieldDescriptor,
  document: SettingsConfigDocument,
): SettingsConfigValue {
  return (
    document[field.key] ??
    field.value ??
    field.defaultValue ??
    (field.type === 'boolean' ? false : '')
  );
}

export function SchemaDrivenSettingsForm({
  fields,
  document,
  disabled = false,
  includeExpert = false,
  onChange,
}: {
  fields: SettingsFieldDescriptor[];
  document: SettingsConfigDocument;
  disabled?: boolean;
  includeExpert?: boolean;
  onChange: (key: string, value: SettingsConfigValue) => void;
}) {
  const visibleFields = fields.filter(
    (field) => includeExpert || (field.visibility ?? 'primary') !== 'expert',
  );
  return (
    <Stack spacing={2}>
      {visibleFields.map((field) => {
        const value = fieldValue(field, document);
        if (field.type === 'boolean') {
          return (
            <Box key={field.key}>
              <FormControlLabel
                control={
                  <Switch
                    checked={Boolean(value)}
                    disabled={disabled}
                    onChange={(event) => onChange(field.key, event.target.checked)}
                  />
                }
                label={field.label}
              />
              <Typography variant="caption" color="text.secondary" sx={{ display: 'block', ml: 6 }}>
                {field.description}
              </Typography>
            </Box>
          );
        }
        if (field.type === 'enum') {
          return (
            <FormControl key={field.key} size="small" fullWidth disabled={disabled}>
              <InputLabel id={`${field.key}-label`}>{field.label}</InputLabel>
              <Select
                labelId={`${field.key}-label`}
                label={field.label}
                value={String(value)}
                onChange={(event) => onChange(field.key, event.target.value)}
              >
                {(field.options ?? []).map((option) => (
                  <MenuItem key={option.value} value={option.value}>
                    {option.label}
                  </MenuItem>
                ))}
              </Select>
              <Typography variant="caption" color="text.secondary" sx={{ mt: 0.6 }}>
                {field.description}
              </Typography>
            </FormControl>
          );
        }
        return (
          <TextField
            key={field.key}
            size="small"
            fullWidth
            disabled={disabled}
            required={field.required}
            type={
              field.type === 'number' ? 'number' : field.type === 'secret' ? 'password' : 'text'
            }
            label={field.label}
            value={value}
            helperText={field.description}
            slotProps={{
              htmlInput: {
                autoCapitalize: 'none',
                autoCorrect: 'off',
                spellCheck: false,
              },
            }}
            onChange={(event) =>
              onChange(
                field.key,
                field.type === 'number' ? Number(event.target.value) : event.target.value,
              )
            }
          />
        );
      })}
    </Stack>
  );
}

export function RawJSONEditor({
  open,
  title,
  document,
  onApply,
  onClose,
}: {
  open: boolean;
  title: string;
  document: SettingsConfigDocument;
  onApply: (value: SettingsConfigDocument) => void;
  onClose: () => void;
}) {
  const [raw, setRaw] = useState(() => JSON.stringify(document, null, 2));
  const [error, setError] = useState('');

  const apply = () => {
    const parsed = parseRawConfiguration(raw);
    if (!parsed.ok) {
      setError(parsed.error);
      return;
    }
    onApply(parsed.value);
    onClose();
  };

  return (
    <Dialog open={open} onClose={onClose} fullWidth maxWidth="sm">
      <DialogTitle>{title}</DialogTitle>
      <DialogContent>
        <Alert severity="info" variant="outlined" sx={{ mb: 2 }}>
          Advanced users can edit the underlying configuration directly. Changes update the same
          preview settings shown in the form.
        </Alert>
        <TextField
          autoFocus
          multiline
          minRows={14}
          fullWidth
          value={raw}
          onChange={(event) => {
            setRaw(event.target.value);
            setError('');
          }}
          error={Boolean(error)}
          helperText={error || 'Invalid JSON is never applied.'}
          slotProps={{
            htmlInput: { 'aria-label': 'Raw JSON configuration', spellCheck: false },
          }}
          sx={{
            '& textarea': {
              fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
              fontSize: 12,
            },
          }}
        />
      </DialogContent>
      <DialogActions>
        <Button onClick={() => setRaw(JSON.stringify(document, null, 2))}>Reset</Button>
        <Button onClick={onClose}>Cancel</Button>
        <Button variant="contained" onClick={apply} startIcon={<CodeRounded />}>
          Apply JSON
        </Button>
      </DialogActions>
    </Dialog>
  );
}
