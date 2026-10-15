import AddRounded from '@mui/icons-material/AddRounded';
import AutoAwesomeRounded from '@mui/icons-material/AutoAwesomeRounded';
import BrightnessAutoRounded from '@mui/icons-material/BrightnessAutoRounded';
import ChevronRightRounded from '@mui/icons-material/ChevronRightRounded';
import DarkModeRounded from '@mui/icons-material/DarkModeRounded';
import DnsRounded from '@mui/icons-material/DnsRounded';
import ExtensionRounded from '@mui/icons-material/ExtensionRounded';
import ExpandMoreRounded from '@mui/icons-material/ExpandMoreRounded';
import HubRounded from '@mui/icons-material/HubRounded';
import LightModeRounded from '@mui/icons-material/LightModeRounded';
import NotificationsNoneRounded from '@mui/icons-material/NotificationsNoneRounded';
import SearchRounded from '@mui/icons-material/SearchRounded';
import SettingsRounded from '@mui/icons-material/SettingsRounded';
import type { SvgIconComponent } from '@mui/icons-material';
import {
  Accordion,
  AccordionDetails,
  AccordionSummary,
  Alert,
  Box,
  Button,
  Chip,
  CircularProgress,
  Collapse,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Divider,
  FormControl,
  FormControlLabel,
  InputAdornment,
  InputLabel,
  List,
  ListItemButton,
  ListItemText,
  MenuItem,
  Paper,
  Select,
  Stack,
  Switch,
  TextField,
  ToggleButton,
  ToggleButtonGroup,
  Typography,
} from '@mui/material';
import { useEffect, useMemo, useState } from 'react';
import type { MouseEvent } from 'react';
import type { AppearanceMode } from '../appearance';
import { useAppearance } from '../appearance';
import { BrowserOpenURL } from '../../wailsjs/runtime/runtime';
import type {
  AutoDismissSeconds,
  CloseWindowBehavior,
  GeneralSettings,
  InAppNotificationSettings,
  NotificationPlacement,
} from '../api/runtimeConfig';
import PageContainer from '../components/layout/PageContainer';
import PageHeader from '../components/layout/PageHeader';
import {
  aiSettingsActions,
  pluginManagementActions,
  settingsDataSource,
} from '../settings/wailsSettingsAdapter';
import { nativeNotificationPermissionWarning } from '../settings/pluginSettingsAdapter';
import {
  addPlugin,
  createPluginInstance,
  removePlugin,
  setPluginInstanceEnabled,
  updatePluginInstanceConfig,
} from '../settings/pluginManagementActions';
import type {
  AddPluginOutcome,
  CreateInstanceOutcome,
  RemovePluginOutcome,
  SetInstanceEnabledOutcome,
  UpdateInstanceConfigOutcome,
} from '../settings/pluginManagementActions';
import {
  DEFAULT_EXTENSION_DISCLOSURE_STATE,
  canRemovePlugin,
  getActionDisplayLabel,
  getAIRuntimeModeDisplayLabel,
  getBuiltInProviderPresentation,
  getCapabilityDisplayLabel,
  getPipelineStageDisplayLabel,
  getProviderSourceDisplayLabel,
  searchSettings,
  updateSettingsValue,
} from '../settings/model';
import type {
  AIMiddlewareBindingView,
  ExtensionView,
  IntegrationInstanceView,
  SettingsConfigDocument,
  SettingsControlPlaneView,
  SettingsLandingCardView,
  SettingsPageId,
  SettingsPlatform,
  SettingsSearchEntry,
} from '../settings/model';
import {
  SchemaDrivenSettingsForm,
  SettingsDetailDrawer,
  SettingsPageHeader,
  SettingsSection,
  SettingsStatus,
} from '../settings/SettingsComponents';

const pageIcons: Record<Exclude<SettingsPageId, 'landing'>, SvgIconComponent> = {
  general: SettingsRounded,
  ai: AutoAwesomeRounded,
  integrations: HubRounded,
  notifications: NotificationsNoneRounded,
  extensions: ExtensionRounded,
  system: DnsRounded,
};

type DetailSelection =
  | { kind: 'middleware'; id: string }
  | { kind: 'integration'; id: string }
  | { kind: 'extension'; id: string }
  | null;

function SettingsLandingCard({
  card,
  onOpen,
}: {
  card: SettingsLandingCardView;
  onOpen: () => void;
}) {
  const Icon = pageIcons[card.id];
  return (
    <Paper
      component="button"
      type="button"
      variant="outlined"
      onClick={onOpen}
      sx={{
        p: 2.25,
        minWidth: 0,
        textAlign: 'left',
        color: 'text.primary',
        cursor: 'pointer',
        transition: 'border-color 150ms ease, transform 150ms ease',
        '&:hover': { borderColor: 'primary.main', transform: 'translateY(-1px)' },
        '&:focus-visible': { outline: '2px solid', outlineColor: 'primary.main', outlineOffset: 2 },
      }}
    >
      <Stack direction="row" spacing={1.5} sx={{ alignItems: 'flex-start' }}>
        <Box
          sx={{
            display: 'grid',
            placeItems: 'center',
            width: 36,
            height: 36,
            flex: '0 0 auto',
            borderRadius: 1,
            bgcolor: 'primary.light',
            color: 'primary.dark',
          }}
        >
          <Icon sx={{ fontSize: 19 }} />
        </Box>
        <Box sx={{ minWidth: 0, flex: 1 }}>
          <Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between' }}>
            <Typography component="h2" variant="h2">
              {card.title}
            </Typography>
            <ChevronRightRounded sx={{ color: 'icon.muted', fontSize: 19 }} />
          </Stack>
          <Typography color="text.secondary" sx={{ mt: 0.5 }}>
            {card.description}
          </Typography>
          <Stack
            direction="row"
            spacing={1}
            sx={{ mt: 1.5, alignItems: 'center', justifyContent: 'space-between' }}
          >
            <SettingsStatus label={card.summary} tone={card.tone} />
            <Typography variant="caption" color="text.secondary" noWrap>
              {card.detail}
            </Typography>
          </Stack>
        </Box>
      </Stack>
    </Paper>
  );
}

function AppearanceCard() {
  const { appearanceMode, setAppearanceMode } = useAppearance();
  const changeAppearance = (_event: MouseEvent<HTMLElement>, mode: AppearanceMode | null) => {
    if (mode) setAppearanceMode(mode);
  };
  return (
    <Paper variant="outlined" sx={{ gridColumn: '1 / -1', p: 2.25 }}>
      <Stack
        direction="row"
        spacing={2}
        useFlexGap
        sx={{ alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap' }}
      >
        <Box>
          <Typography component="h2" variant="h2">
            Appearance
          </Typography>
          <Typography color="text.secondary" sx={{ mt: 0.5 }}>
            Follow the system appearance or choose a fixed theme.
          </Typography>
        </Box>
        <ToggleButtonGroup
          exclusive
          size="small"
          value={appearanceMode}
          onChange={changeAppearance}
          aria-label="Application appearance"
        >
          <ToggleButton value="system" aria-label="System appearance">
            <BrightnessAutoRounded sx={{ mr: 0.75, fontSize: 17 }} /> System
          </ToggleButton>
          <ToggleButton value="light" aria-label="Light appearance">
            <LightModeRounded sx={{ mr: 0.75, fontSize: 17 }} /> Light
          </ToggleButton>
          <ToggleButton value="dark" aria-label="Dark appearance">
            <DarkModeRounded sx={{ mr: 0.75, fontSize: 17 }} /> Dark
          </ToggleButton>
        </ToggleButtonGroup>
      </Stack>
    </Paper>
  );
}

function LandingPage({
  view,
  query,
  results,
  onQueryChange,
  onOpenPage,
  onSelectResult,
}: {
  view: SettingsControlPlaneView;
  query: string;
  results: SettingsSearchEntry[];
  onQueryChange: (value: string) => void;
  onOpenPage: (page: Exclude<SettingsPageId, 'landing'>) => void;
  onSelectResult: (entry: SettingsSearchEntry) => void;
}) {
  return (
    <>
      <PageHeader
        eyebrow="Personalize Lum Harbor"
        title="Settings"
        subtitle="Manage how Lum Harbor looks, connects, and works."
        status={view.sourceLabel}
      />
      <Box sx={{ position: 'relative', mb: 2.25 }}>
        <TextField
          fullWidth
          size="small"
          value={query}
          placeholder="Search settings..."
          onChange={(event) => onQueryChange(event.target.value)}
          slotProps={{
            htmlInput: { 'aria-label': 'Search settings' },
            input: {
              startAdornment: (
                <InputAdornment position="start">
                  <SearchRounded color="action" fontSize="small" />
                </InputAdornment>
              ),
            },
          }}
        />
        {query.trim() && (
          <Paper
            variant="outlined"
            sx={{
              position: 'absolute',
              zIndex: 5,
              top: 'calc(100% + 6px)',
              left: 0,
              right: 0,
              p: 0.5,
              maxHeight: 340,
              overflowY: 'auto',
            }}
          >
            {results.length ? (
              <List disablePadding aria-label="Settings search results">
                {results.map((entry) => (
                  <ListItemButton
                    key={entry.id}
                    onClick={() => onSelectResult(entry)}
                    sx={{ borderRadius: 1 }}
                  >
                    <ListItemText
                      primary={entry.label}
                      secondary={`${entry.category} › ${entry.section}`}
                      slotProps={{ primary: { sx: { fontSize: 13, fontWeight: 600 } } }}
                    />
                    <ChevronRightRounded color="action" fontSize="small" />
                  </ListItemButton>
                ))}
              </List>
            ) : (
              <Typography color="text.secondary" sx={{ p: 1.5 }}>
                No settings found. Try provider, timeout, reminder, or extension.
              </Typography>
            )}
          </Paper>
        )}
      </Box>
      <Box
        sx={{
          display: 'grid',
          gridTemplateColumns: 'minmax(0, 1fr)',
          gap: 2.25,
          '@container (min-width: 760px)': { gridTemplateColumns: 'repeat(2, minmax(0, 1fr))' },
        }}
      >
        <AppearanceCard />
        {view.landingCards.map((card) => (
          <SettingsLandingCard key={card.id} card={card} onOpen={() => onOpenPage(card.id)} />
        ))}
      </Box>
    </>
  );
}

function AIPage({
  view,
  platform,
  highlighted,
  onBack,
  onOpenMiddleware,
  onOpenExtension,
}: {
  view: SettingsControlPlaneView['ai'];
  platform: SettingsPlatform;
  highlighted: string | null;
  onBack: () => void;
  onOpenMiddleware: (id: string) => void;
  onOpenExtension?: (id: string) => void;
}) {
  const presentation = getBuiltInProviderPresentation(view.runtime);
  const [showBuiltIn, setShowBuiltIn] = useState(
    view.runtime.builtInConfigEditable || !presentation.collapsedByDefault,
  );
  const [builtInConfig, setBuiltInConfig] = useState(view.builtInProvider.config);
  const [useSystemCredentialManager, setUseSystemCredentialManager] = useState(
    view.builtInProvider.useSystemCredentialManager,
  );
  const [credentialConfigured, setCredentialConfigured] = useState(
    view.builtInProvider.credentialConfigured,
  );
  const [credentialDraft, setCredentialDraft] = useState('');
  const [restartRequired, setRestartRequired] = useState(view.builtInProvider.restartRequired);
  const [settingsMessage, setSettingsMessage] = useState<string | null>(null);
  const [savingAI, setSavingAI] = useState(false);
  const [verification, setVerification] = useState<{
    success: boolean;
    message: string;
    effectiveProviderName: string;
  } | null>(null);
  const [verifying, setVerifying] = useState(false);
  const pre = view.middleware.filter((item) => item.stage === 'PRE');
  const post = view.middleware.filter((item) => item.stage === 'POST');

  return (
    <>
      <SettingsPageHeader
        title="AI"
        description="See which provider handles AI requests and how Lum Harbor processes them."
        onBack={onBack}
        status={
          <SettingsStatus
            label={view.runtime.status}
            tone={view.runtime.status === 'Ready' ? 'healthy' : 'attention'}
          />
        }
      />
      <Stack spacing={2.25}>
        <SettingsSection
          id="ai-runtime"
          title="Runtime"
          description="The provider currently handling AI features."
          highlighted={highlighted === 'ai-runtime'}
        >
          <Stack
            direction={{ xs: 'column', sm: 'row' }}
            spacing={3}
            sx={{ justifyContent: 'space-between' }}
          >
            <Box>
              <Typography variant="caption" color="text.secondary">
                Effective provider
              </Typography>
              <Typography sx={{ mt: 0.35, fontWeight: 650 }}>
                {view.runtime.effectiveProviderName}
              </Typography>
            </Box>
            <Box>
              <Typography variant="caption" color="text.secondary">
                Provider setup
              </Typography>
              <Typography sx={{ mt: 0.35, fontWeight: 650 }}>
                {getAIRuntimeModeDisplayLabel(view.runtime.mode)}
              </Typography>
              <Typography variant="caption" color="text.secondary">
                {getProviderSourceDisplayLabel(view.runtime.providerSource)}
              </Typography>
            </Box>
            <Alert
              severity={view.runtime.status === 'Ready' ? 'info' : 'warning'}
              variant="outlined"
              sx={{ py: 0, maxWidth: 360 }}
            >
              {view.runtime.issue ??
                (view.runtime.mode !== 'BUILTIN'
                  ? `All AI requests are currently handled by ${view.runtime.managedByExtension}.`
                  : 'The built-in provider is currently handling AI requests.')}
            </Alert>
            <Box>
              <Button
                variant="outlined"
                disabled={verifying}
                onClick={() => {
                  setVerifying(true);
                  setVerification(null);
                  void aiSettingsActions
                    .verifyRuntime()
                    .then((result) =>
                      setVerification({
                        success: result.success,
                        message: result.message,
                        effectiveProviderName: result.effectiveProviderName,
                      }),
                    )
                    .catch((error: unknown) =>
                      setVerification({
                        success: false,
                        message: error instanceof Error ? error.message : String(error),
                        effectiveProviderName: view.runtime.effectiveProviderName,
                      }),
                    )
                    .finally(() => setVerifying(false));
                }}
              >
                {verifying ? 'Verifying…' : 'Verify AI Runtime'}
              </Button>
            </Box>
          </Stack>
          {verification && (
            <Alert severity={verification.success ? 'success' : 'error'} sx={{ mt: 2 }}>
              {verification.message} ·{' '}
              {verification.effectiveProviderName || 'No effective provider'}
            </Alert>
          )}
          {view.runtime.mode !== 'BUILTIN' && view.runtime.managingExtensionId && (
            <Paper variant="outlined" sx={{ p: 2, mt: 2 }}>
              <Stack
                direction="row"
                spacing={2}
                sx={{ alignItems: 'center', justifyContent: 'space-between' }}
              >
                <Box>
                  <Typography variant="body2" color="text.secondary">
                    AI is managed by:{' '}
                    <Typography component="span" variant="body2" sx={{ fontWeight: 600 }}>
                      {view.runtime.managedByExtension ||
                        view.runtime.effectiveProviderName ||
                        view.runtime.managingExtensionId}
                    </Typography>
                  </Typography>
                </Box>
                {onOpenExtension && (
                  <Button
                    variant="outlined"
                    size="small"
                    onClick={() => onOpenExtension(view.runtime.managingExtensionId!)}
                  >
                    Configure Plugin
                  </Button>
                )}
              </Stack>
            </Paper>
          )}
        </SettingsSection>

        <SettingsSection
          id="ai-pipeline"
          title="AI Pipeline"
          description="AI requests pass through these steps in order."
          highlighted={highlighted === 'ai-pipeline'}
        >
          <Stack spacing={1}>
            {[...pre, view.primaryStage, ...post].map((item, index) => (
              <Paper
                key={item.id}
                component="button"
                type="button"
                disabled={item.stage === 'PROVIDER' || item.stage === 'RUNTIME' || !item.enabled}
                variant="outlined"
                sx={{
                  p: 1.5,
                  width: '100%',
                  textAlign: 'left',
                  color: 'text.primary',
                  font: 'inherit',
                  bgcolor: 'surface.subtle',
                  cursor:
                    item.stage === 'PROVIDER' || item.stage === 'RUNTIME' || !item.enabled
                      ? 'default'
                      : 'pointer',
                  '&:focus-visible': {
                    outline: '2px solid',
                    outlineColor: 'primary.main',
                    outlineOffset: 2,
                  },
                }}
                onClick={() =>
                  item.stage !== 'PROVIDER' &&
                  item.stage !== 'RUNTIME' &&
                  item.enabled &&
                  onOpenMiddleware(item.id)
                }
              >
                <Stack direction="row" spacing={1.5} sx={{ alignItems: 'center' }}>
                  <Typography variant="caption" color="text.secondary" sx={{ width: 24 }}>
                    {index + 1}
                  </Typography>
                  <Chip
                    label={getPipelineStageDisplayLabel(item.stage)}
                    size="small"
                    variant="outlined"
                    sx={{ minWidth: 92 }}
                  />
                  <Box sx={{ minWidth: 0, flex: 1 }}>
                    <Typography sx={{ fontWeight: 650 }}>{item.name}</Typography>
                    <Typography variant="caption" color="text.secondary">
                      {item.extensionName} ·{' '}
                      {item.actions === 'ALL'
                        ? 'All AI features'
                        : item.actions.map(getActionDisplayLabel).join(', ')}
                    </Typography>
                  </Box>
                  <SettingsStatus
                    label={item.enabled ? 'Enabled' : 'Disabled'}
                    tone={item.enabled ? 'healthy' : 'inactive'}
                  />
                </Stack>
              </Paper>
            ))}
          </Stack>
        </SettingsSection>

        <SettingsSection
          id="ai-built-in"
          title={view.builtInProvider.name}
          description={view.builtInProvider.savedSummary}
          highlighted={highlighted === 'ai-built-in'}
          action={
            <SettingsStatus
              label={presentation.statusLabel}
              tone={presentation.active ? 'healthy' : 'inactive'}
            />
          }
        >
          <Typography color="text.secondary">{presentation.helperText}</Typography>
          <Stack direction="row" spacing={1} sx={{ mt: 1, alignItems: 'center' }}>
            <SettingsStatus
              label={credentialConfigured ? 'Credential configured' : 'Credential missing'}
              tone={credentialConfigured ? 'healthy' : 'attention'}
            />
            {restartRequired && <Chip size="small" color="warning" label="Restart required" />}
          </Stack>
          {platform === 'MACOS' && (
            <Box sx={{ mt: 1 }}>
              <FormControlLabel
                control={
                  <Switch
                    checked={useSystemCredentialManager}
                    disabled={!view.runtime.builtInConfigEditable || savingAI}
                    onChange={(event) => {
                      const nextValue = event.target.checked;
                      setSavingAI(true);
                      setSettingsMessage(null);
                      void aiSettingsActions
                        .saveSettings({
                          endpoint: String(builtInConfig.endpoint ?? ''),
                          model: String(builtInConfig.model ?? ''),
                          requestTimeoutSeconds: Number(builtInConfig.requestTimeoutSeconds ?? 30),
                          useSystemCredentialManager: nextValue,
                        })
                        .then((result) => {
                          setUseSystemCredentialManager(result.useSystemCredentialManager);
                          setCredentialConfigured(result.credentialConfigured);
                          setRestartRequired(result.restartRequired);
                          setSettingsMessage(
                            nextValue
                              ? 'Now using the system credential manager. Restart to activate it.'
                              : 'Now using the Lum Harbor application-managed credential store. Restart to activate it.',
                          );
                        })
                        .catch((error: unknown) =>
                          setSettingsMessage(
                            error instanceof Error ? error.message : String(error),
                          ),
                        )
                        .finally(() => setSavingAI(false));
                    }}
                  />
                }
                label="Use system credential manager"
              />
              {!useSystemCredentialManager && (
                <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>
                  The application-managed store is protected only by file permissions, not the
                  system Keychain.
                </Typography>
              )}
            </Box>
          )}
          {!presentation.active && (
            <Button onClick={() => setShowBuiltIn((shown) => !shown)} sx={{ mt: 1.2, px: 0.5 }}>
              {showBuiltIn ? 'Hide inactive settings' : 'Show inactive settings'}
            </Button>
          )}
          <Collapse in={showBuiltIn}>
            <Box sx={{ mt: 2, maxWidth: 620, opacity: presentation.active ? 1 : 0.68 }}>
              <SchemaDrivenSettingsForm
                fields={view.builtInProvider.fields}
                document={builtInConfig}
                disabled={!view.runtime.builtInConfigEditable || savingAI}
                onChange={(key, value) =>
                  setBuiltInConfig((current) => updateSettingsValue(current, key, value))
                }
              />
              <Stack spacing={1.25} sx={{ mt: 2 }}>
                <Button
                  variant="contained"
                  disabled={!view.runtime.builtInConfigEditable || savingAI}
                  onClick={() => {
                    setSavingAI(true);
                    setSettingsMessage(null);
                    void aiSettingsActions
                      .saveSettings({
                        endpoint: String(builtInConfig.endpoint ?? ''),
                        model: String(builtInConfig.model ?? ''),
                        requestTimeoutSeconds: Number(builtInConfig.requestTimeoutSeconds ?? 30),
                        useSystemCredentialManager,
                      })
                      .then((result) => {
                        setRestartRequired(result.restartRequired);
                        setSettingsMessage('Configuration saved. Restart to activate it.');
                      })
                      .catch((error: unknown) =>
                        setSettingsMessage(error instanceof Error ? error.message : String(error)),
                      )
                      .finally(() => setSavingAI(false));
                  }}
                >
                  Save configuration
                </Button>
                <TextField
                  type="password"
                  size="small"
                  label={credentialConfigured ? 'Replace API credential' : 'API credential'}
                  value={credentialDraft}
                  disabled={!view.runtime.builtInConfigEditable || savingAI}
                  onChange={(event) => setCredentialDraft(event.target.value)}
                  helperText="Write-only. Lum Harbor never reads the credential back into Settings."
                />
                <Stack direction="row" spacing={1}>
                  <Button
                    variant="outlined"
                    disabled={
                      !view.runtime.builtInConfigEditable || savingAI || !credentialDraft.trim()
                    }
                    onClick={() => {
                      setSavingAI(true);
                      setSettingsMessage(null);
                      void aiSettingsActions
                        .setCredential({ credential: credentialDraft })
                        .then((result) => {
                          setCredentialConfigured(result.credentialConfigured);
                          setRestartRequired(result.restartRequired);
                          setCredentialDraft('');
                          setSettingsMessage('Credential saved securely. Restart to activate it.');
                        })
                        .catch((error: unknown) =>
                          setSettingsMessage(
                            error instanceof Error ? error.message : String(error),
                          ),
                        )
                        .finally(() => setSavingAI(false));
                    }}
                  >
                    {credentialConfigured ? 'Replace credential' : 'Save credential'}
                  </Button>
                  {credentialConfigured && (
                    <Button
                      color="error"
                      disabled={!view.runtime.builtInConfigEditable || savingAI}
                      onClick={() => {
                        setSavingAI(true);
                        setSettingsMessage(null);
                        void aiSettingsActions
                          .deleteCredential()
                          .then((result) => {
                            setCredentialConfigured(result.credentialConfigured);
                            setRestartRequired(result.restartRequired);
                            setSettingsMessage(
                              'Credential removed. Restart to activate the change.',
                            );
                          })
                          .catch((error: unknown) =>
                            setSettingsMessage(
                              error instanceof Error ? error.message : String(error),
                            ),
                          )
                          .finally(() => setSavingAI(false));
                      }}
                    >
                      Remove credential
                    </Button>
                  )}
                </Stack>
                {settingsMessage && <Alert severity="info">{settingsMessage}</Alert>}
              </Stack>
            </Box>
          </Collapse>
        </SettingsSection>

        <SettingsSection
          id="ai-behavior"
          title="Behavior"
          description="Behavior options will appear when they are available."
        >
          <Typography color="text.secondary">No behavior settings are available yet.</Typography>
        </SettingsSection>
      </Stack>
    </>
  );
}

function IntegrationsPage({
  view,
  highlighted,
  onBack,
  onOpen,
}: {
  view: SettingsControlPlaneView['integrations'];
  highlighted: string | null;
  onBack: () => void;
  onOpen: (id: string) => void;
}) {
  return (
    <>
      <SettingsPageHeader
        title="Integrations"
        description="Manage each connected account or workspace separately."
        onBack={onBack}
        status={<SettingsStatus label={`${view.connected.length} connected`} tone="neutral" />}
      />
      <Stack spacing={2.25}>
        <SettingsSection
          id="integrations-connected"
          title="Connected"
          description="Each connection has its own settings and status, even when several use the same service."
          highlighted={highlighted === 'integrations-connected'}
        >
          <Stack spacing={1}>
            {view.connected.map((integration) => (
              <Paper
                key={integration.id}
                component="button"
                type="button"
                variant="outlined"
                sx={{
                  p: 1.75,
                  width: '100%',
                  textAlign: 'left',
                  color: 'text.primary',
                  font: 'inherit',
                  cursor: 'pointer',
                  bgcolor: 'surface.subtle',
                  '&:focus-visible': {
                    outline: '2px solid',
                    outlineColor: 'primary.main',
                    outlineOffset: 2,
                  },
                }}
                onClick={() => onOpen(integration.id)}
              >
                <Stack direction="row" spacing={2} sx={{ alignItems: 'center' }}>
                  <Box sx={{ minWidth: 0, flex: 1 }}>
                    <Typography sx={{ fontWeight: 650 }}>
                      {integration.typeName} · {integration.instanceName}
                    </Typography>
                    <Typography variant="caption" color="text.secondary">
                      {integration.detail}
                      {integration.lastActivity ? ` · ${integration.lastActivity}` : ''}
                    </Typography>
                  </Box>
                  <SettingsStatus label={integration.status} tone={integration.health} />
                  <ChevronRightRounded color="action" fontSize="small" />
                </Stack>
              </Paper>
            ))}
            {view.connected.length === 0 && (
              <Typography color="text.secondary">No configured Plugin instances.</Typography>
            )}
          </Stack>
        </SettingsSection>
        <SettingsSection
          id="integrations-available"
          title="Available"
          description="More connection options will become available in a future update."
          action={
            <Button startIcon={<AddRounded />} disabled>
              Add integration
            </Button>
          }
        >
          <Stack direction={{ xs: 'column', md: 'row' }} spacing={1.5}>
            {view.available.map((item) => (
              <Paper
                key={item.typeId}
                variant="outlined"
                sx={{ p: 1.75, flex: 1, bgcolor: 'surface.subtle' }}
              >
                <Typography sx={{ fontWeight: 650 }}>{item.name}</Typography>
                <Typography color="text.secondary" sx={{ mt: 0.5 }}>
                  {item.description}
                </Typography>
                <Typography
                  variant="caption"
                  color="text.secondary"
                  sx={{ display: 'block', mt: 1 }}
                >
                  Provided by {item.extensionName}
                </Typography>
              </Paper>
            ))}
          </Stack>
        </SettingsSection>
      </Stack>
    </>
  );
}

function AddInstanceDialog({
  extension,
  open,
  submitting,
  error,
  onClose,
  onSubmit,
}: {
  extension: ExtensionView | null;
  open: boolean;
  submitting: boolean;
  error: string | null;
  onClose: () => void;
  onSubmit: (capabilityId: string, document: SettingsConfigDocument) => void;
}) {
  if (!extension) return null;
  return (
    <Dialog open={open} onClose={submitting ? undefined : onClose} fullWidth maxWidth="sm">
      {/* Keying on the extension id gives each newly opened Plugin a fresh
          capability/config draft without resetting state in an effect. */}
      <AddInstanceDialogContent
        key={extension.id}
        extension={extension}
        submitting={submitting}
        error={error}
        onClose={onClose}
        onSubmit={onSubmit}
      />
    </Dialog>
  );
}

function AddInstanceDialogContent({
  extension,
  submitting,
  error,
  onClose,
  onSubmit,
}: {
  extension: ExtensionView;
  submitting: boolean;
  error: string | null;
  onClose: () => void;
  onSubmit: (capabilityId: string, document: SettingsConfigDocument) => void;
}) {
  const [capabilityId, setCapabilityId] = useState(extension.selectableCapabilities[0] ?? '');
  const [document, setDocument] = useState<SettingsConfigDocument>({});

  const configurableFields = extension.fields.filter((field) => field.type !== 'secret');
  const hasRequiredSecret = extension.fields.some(
    (field) => field.type === 'secret' && field.required,
  );
  const hasSelectableCapability = extension.selectableCapabilities.length > 0;
  const canSubmit =
    hasSelectableCapability && !hasRequiredSecret && capabilityId !== '' && !submitting;

  return (
    <>
      <DialogTitle>Use {extension.displayName}</DialogTitle>
      <DialogContent>
        <Stack spacing={2.5} sx={{ mt: 0.5 }}>
          {!hasSelectableCapability && (
            <Alert severity="warning" variant="outlined">
              Lum Harbor does not support any capability this Plugin provides yet.
            </Alert>
          )}
          {hasSelectableCapability && hasRequiredSecret && (
            <Alert severity="warning" variant="outlined">
              This Plugin requires credential configuration, which Lum Harbor does not support yet.
            </Alert>
          )}
          {hasSelectableCapability &&
            !hasRequiredSecret &&
            extension.fields.some((f) => f.type === 'secret') && (
              <Alert severity="info" variant="outlined">
                Secret fields (such as API keys) cannot be entered here and are externally managed
                via environment variables (e.g. LUM_HARBOR_OPENAI_API_KEY) or optional for local
                runtimes.
              </Alert>
            )}
          {hasSelectableCapability && !hasRequiredSecret && (
            <>
              {extension.selectableCapabilities.length > 1 ? (
                <FormControl size="small" fullWidth disabled={submitting}>
                  <InputLabel id="add-instance-capability-label">Capability</InputLabel>
                  <Select
                    labelId="add-instance-capability-label"
                    label="Capability"
                    value={capabilityId}
                    onChange={(event) => setCapabilityId(event.target.value)}
                  >
                    {extension.selectableCapabilities.map((capability) => (
                      <MenuItem key={capability} value={capability}>
                        {getCapabilityDisplayLabel(capability)}
                      </MenuItem>
                    ))}
                  </Select>
                </FormControl>
              ) : (
                <Typography color="text.secondary">
                  Capability: {getCapabilityDisplayLabel(capabilityId)}
                </Typography>
              )}
              {configurableFields.length > 0 && (
                <SchemaDrivenSettingsForm
                  fields={configurableFields}
                  document={document}
                  disabled={submitting}
                  onChange={(key, value) =>
                    setDocument((current) => updateSettingsValue(current, key, value))
                  }
                />
              )}
            </>
          )}
          {error && (
            <Alert severity="error" variant="outlined">
              {error}
            </Alert>
          )}
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} disabled={submitting}>
          Cancel
        </Button>
        <Button
          variant="contained"
          disabled={!canSubmit}
          startIcon={submitting ? <CircularProgress size={14} color="inherit" /> : undefined}
          onClick={() => onSubmit(capabilityId, document)}
        >
          Save
        </Button>
      </DialogActions>
    </>
  );
}

function EditInstanceDialog({
  target,
  open,
  submitting,
  error,
  onClose,
  onSubmit,
}: {
  target: {
    extension: ExtensionView;
    instance: ExtensionView['instances'][number];
  } | null;
  open: boolean;
  submitting: boolean;
  error: string | null;
  onClose: () => void;
  onSubmit: (instanceId: string, document: SettingsConfigDocument) => void;
}) {
  if (!target) return null;
  return (
    <Dialog open={open} onClose={submitting ? undefined : onClose} fullWidth maxWidth="sm">
      <EditInstanceDialogContent
        key={target.instance.id}
        extension={target.extension}
        instance={target.instance}
        submitting={submitting}
        error={error}
        onClose={onClose}
        onSubmit={onSubmit}
      />
    </Dialog>
  );
}

function EditInstanceDialogContent({
  extension,
  instance,
  submitting,
  error,
  onClose,
  onSubmit,
}: {
  extension: ExtensionView;
  instance: ExtensionView['instances'][number];
  submitting: boolean;
  error: string | null;
  onClose: () => void;
  onSubmit: (instanceId: string, document: SettingsConfigDocument) => void;
}) {
  const [document, setDocument] = useState<SettingsConfigDocument>(() => ({
    ...(instance.config ?? {}),
  }));

  const configurableFields = extension.fields.filter((field) => field.type !== 'secret');
  const secretFields = extension.fields.filter((field) => field.type === 'secret');

  return (
    <>
      <DialogTitle>Configure Instance</DialogTitle>
      <DialogContent>
        <Stack spacing={2.5} sx={{ mt: 0.5 }}>
          <Box>
            <Typography variant="body2" sx={{ fontWeight: 650 }}>
              {instance.id}
            </Typography>
            <Typography variant="caption" color="text.secondary">
              Plugin: {extension.displayName} · Capability:{' '}
              {getCapabilityDisplayLabel(instance.capabilityId)}
            </Typography>
          </Box>
          {secretFields.length > 0 && (
            <Alert severity="info" variant="outlined">
              Secret fields ({secretFields.map((f) => f.label).join(', ')}) cannot be stored in
              instance configuration and are externally managed via environment variables (such as
              LUM_HARBOR_OPENAI_API_KEY) or optional for local runtimes.
            </Alert>
          )}
          {configurableFields.length > 0 ? (
            <SchemaDrivenSettingsForm
              fields={configurableFields}
              document={document}
              disabled={submitting}
              onChange={(key, value) =>
                setDocument((current) => updateSettingsValue(current, key, value))
              }
            />
          ) : (
            <Typography color="text.secondary">
              This instance has no configurable settings.
            </Typography>
          )}
          {error && (
            <Alert severity="error" variant="outlined">
              {error}
            </Alert>
          )}
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} disabled={submitting}>
          Cancel
        </Button>
        <Button
          variant="contained"
          disabled={submitting}
          startIcon={submitting ? <CircularProgress size={14} color="inherit" /> : undefined}
          onClick={() => onSubmit(instance.id, document)}
        >
          Save
        </Button>
      </DialogActions>
    </>
  );
}

function RemovePluginDialog({
  extension,
  open,
  submitting,
  error,
  onClose,
  onConfirm,
}: {
  extension: ExtensionView | null;
  open: boolean;
  submitting: boolean;
  error: string | null;
  onClose: () => void;
  onConfirm: () => void;
}) {
  if (!extension) return null;
  return (
    <Dialog open={open} onClose={submitting ? undefined : onClose} fullWidth maxWidth="sm">
      <DialogTitle>Remove {extension.displayName}?</DialogTitle>
      <DialogContent>
        <Stack spacing={2} sx={{ mt: 0.5 }}>
          <Typography color="text.secondary">
            This removes the {extension.displayName} installation from Lum Harbor and deletes its
            disabled configurations. Lum Harbor keeps everything it has already recorded from this
            Plugin — Source data, history, and other saved evidence are not affected. Restart Lum
            Harbor to finish removing it.
          </Typography>
          {error && (
            <Alert severity="error" variant="outlined">
              {error}
            </Alert>
          )}
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} disabled={submitting}>
          Cancel
        </Button>
        <Button
          variant="contained"
          color="error"
          disabled={submitting}
          startIcon={submitting ? <CircularProgress size={14} color="inherit" /> : undefined}
          onClick={onConfirm}
        >
          Remove Plugin
        </Button>
      </DialogActions>
    </Dialog>
  );
}

function ExtensionsPage({
  extensions,
  highlighted,
  onBack,
  onOpen,
  installing,
  installOutcome,
  onAddPlugin,
  onDismissInstallNotice,
  instanceCreated,
  onDismissInstanceNotice,
  pluginRemoved,
  onDismissRemovedNotice,
}: {
  extensions: ExtensionView[];
  highlighted: string | null;
  onBack: () => void;
  onOpen: (id: string) => void;
  installing: boolean;
  installOutcome: AddPluginOutcome | null;
  onAddPlugin: () => void;
  onDismissInstallNotice: () => void;
  instanceCreated: { extensionId: string; capabilityId: string } | null;
  onDismissInstanceNotice: () => void;
  pluginRemoved: { extensionId: string } | null;
  onDismissRemovedNotice: () => void;
}) {
  return (
    <>
      <SettingsPageHeader
        title="Extensions"
        description="Review installed Plugins, configured instances, and current process state."
        onBack={onBack}
        status={<SettingsStatus label={`${extensions.length} installed`} tone="healthy" />}
      />
      <SettingsSection
        id="extensions-installed"
        title="Installed"
        description="Open a Plugin to review its capabilities and configured instances."
        highlighted={highlighted === 'extensions-installed'}
        action={
          <Button
            variant="contained"
            size="small"
            startIcon={installing ? <CircularProgress size={14} color="inherit" /> : <AddRounded />}
            disabled={installing}
            onClick={onAddPlugin}
          >
            Add Plugin
          </Button>
        }
      >
        <Stack spacing={1.25}>
          {installOutcome?.status === 'installed' && (
            <Alert severity="success" variant="outlined" onClose={onDismissInstallNotice}>
              Installed {installOutcome.result.extensionId} v{installOutcome.result.version}.{' '}
              {installOutcome.result.restartRequired
                ? 'Restart Lum Harbor to make this Plugin available.'
                : ''}
            </Alert>
          )}
          {installOutcome?.status === 'error' && (
            <Alert severity="error" variant="outlined" onClose={onDismissInstallNotice}>
              {installOutcome.message}
            </Alert>
          )}
          {instanceCreated && (
            <Alert severity="success" variant="outlined" onClose={onDismissInstanceNotice}>
              Configured {getCapabilityDisplayLabel(instanceCreated.capabilityId)} for{' '}
              {instanceCreated.extensionId}. Restart Lum Harbor to start using this Plugin.
            </Alert>
          )}
          {pluginRemoved && (
            <Alert severity="success" variant="outlined" onClose={onDismissRemovedNotice}>
              Removed {pluginRemoved.extensionId}. Restart Lum Harbor to finish removing this Plugin
              from the app.
            </Alert>
          )}
          {extensions.map((extension) => (
            <Paper
              key={extension.id}
              component="button"
              type="button"
              variant="outlined"
              sx={{
                p: 1.75,
                width: '100%',
                textAlign: 'left',
                color: 'text.primary',
                font: 'inherit',
                cursor: 'pointer',
                bgcolor: 'surface.subtle',
                '&:focus-visible': {
                  outline: '2px solid',
                  outlineColor: 'primary.main',
                  outlineOffset: 2,
                },
              }}
              onClick={() => onOpen(extension.id)}
            >
              <Stack direction="row" spacing={2} sx={{ alignItems: 'flex-start' }}>
                <Box sx={{ minWidth: 0, flex: 1 }}>
                  <Stack
                    direction="row"
                    spacing={1}
                    sx={{ alignItems: 'center', flexWrap: 'wrap' }}
                  >
                    <Typography sx={{ fontWeight: 650 }}>{extension.displayName}</Typography>
                    <Typography variant="caption" color="text.secondary">
                      v{extension.version}
                    </Typography>
                  </Stack>
                  <Typography color="text.secondary" sx={{ mt: 0.45 }}>
                    {extension.summary}
                  </Typography>
                  <Stack
                    direction="row"
                    spacing={0.75}
                    useFlexGap
                    sx={{ mt: 1.1, flexWrap: 'wrap' }}
                  >
                    {extension.capabilities.map((capability) => (
                      <Chip
                        key={capability}
                        size="small"
                        variant="outlined"
                        label={getCapabilityDisplayLabel(capability)}
                      />
                    ))}
                  </Stack>
                </Box>
                <SettingsStatus label={extension.status} tone={extension.health} />
                <ChevronRightRounded color="action" fontSize="small" />
              </Stack>
            </Paper>
          ))}
          {extensions.length === 0 && (
            <Typography color="text.secondary">No installed Plugins were discovered.</Typography>
          )}
        </Stack>
      </SettingsSection>
    </>
  );
}

function SystemPage({
  view,
  highlighted,
  onBack,
}: {
  view: SettingsControlPlaneView['system'];
  highlighted: string | null;
  onBack: () => void;
}) {
  return (
    <>
      <SettingsPageHeader
        title="System"
        description="Review application services, local data, and background activity."
        onBack={onBack}
        status={<SettingsStatus label={view.startupStatus} tone="healthy" />}
      />
      <Stack spacing={2.25}>
        <SettingsSection
          id="system-runtime"
          title="Runtime"
          description="Live service details will appear when system monitoring is connected."
        >
          <Stack spacing={1.1}>
            {[
              ['Startup', view.startupStatus],
              ['Configuration', view.configStatus],
              ['Database', view.databaseStatus],
            ].map(([label, value]) => (
              <Stack key={label} direction="row" sx={{ justifyContent: 'space-between' }}>
                <Typography color="text.secondary">{label}</Typography>
                <Typography sx={{ fontWeight: 600 }}>{value}</Typography>
              </Stack>
            ))}
          </Stack>
        </SettingsSection>
        <SettingsSection
          id="system-background"
          title="Background Runtime"
          description={view.backgroundStatus}
          highlighted={highlighted === 'system-background'}
        >
          <Stack spacing={1}>
            {view.jobs.map((job) => (
              <Paper key={job.id} variant="outlined" sx={{ p: 1.5, bgcolor: 'surface.subtle' }}>
                <Stack direction="row" sx={{ justifyContent: 'space-between' }}>
                  <Typography sx={{ fontWeight: 600 }}>{job.label}</Typography>
                  <SettingsStatus label={job.status} tone="inactive" />
                </Stack>
                <Typography variant="caption" color="text.secondary">
                  {job.detail}
                </Typography>
              </Paper>
            ))}
          </Stack>
        </SettingsSection>
      </Stack>
    </>
  );
}

const closeWindowBehaviorOptions: Array<{ value: CloseWindowBehavior; label: string }> = [
  { value: 'background', label: 'Keep Lum Harbor running in background' },
  { value: 'quit', label: 'Quit Lum Harbor' },
];

function GeneralPage({
  settings,
  onSave,
  onBack,
}: {
  settings: GeneralSettings;
  onSave?: (value: GeneralSettings) => Promise<void>;
  onBack: () => void;
}) {
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const save = async (next: GeneralSettings) => {
    if (saving || !onSave) return;
    setSaving(true);
    setMessage(null);
    setError(null);
    try {
      await onSave(next);
      setMessage('General preferences saved.');
    } catch (saveError) {
      setError(saveError instanceof Error ? saveError.message : String(saveError));
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      <SettingsPageHeader
        title="General"
        description="Configure window lifecycle and desktop behavior."
        onBack={onBack}
        status={<SettingsStatus label="Window lifecycle" tone="healthy" />}
      />
      <SettingsSection
        id="general-window-close"
        title="Window behavior"
        description="Choose what happens when you close the main Lum Harbor window."
      >
        <Stack spacing={2.25}>
          <Box>
            <Typography sx={{ fontWeight: 650 }}>When closing the window</Typography>
            <Typography variant="body2" color="text.secondary" sx={{ mt: 0.35, mb: 1.25 }}>
              Choose whether Lum Harbor remains running in the background when the main window is
              closed.
            </Typography>
            <FormControl size="small" sx={{ minWidth: 320, maxWidth: 440 }}>
              <InputLabel id="close-window-behavior-label">When closing the window</InputLabel>
              <Select
                labelId="close-window-behavior-label"
                label="When closing the window"
                value={settings.closeWindowBehavior}
                disabled={saving || !onSave}
                onChange={(event) =>
                  void save({
                    ...settings,
                    closeWindowBehavior: event.target.value as CloseWindowBehavior,
                  })
                }
              >
                {closeWindowBehaviorOptions.map((option) => (
                  <MenuItem key={option.value} value={option.value}>
                    {option.label}
                  </MenuItem>
                ))}
              </Select>
            </FormControl>
          </Box>

          <Alert severity="info" variant="outlined">
            {settings.closeWindowBehavior === 'background'
              ? 'Closing the main window hides it while Scheduler, Plugins, and Notifications continue running. Explicit Quit (Cmd+Q or the menu bar item) will always shut down the application.'
              : 'Closing the main window will completely quit Lum Harbor and terminate all background tasks.'}
          </Alert>

          {message && (
            <Alert severity="success" variant="outlined" onClose={() => setMessage(null)}>
              {message}
            </Alert>
          )}
          {error && (
            <Alert severity="error" variant="outlined" onClose={() => setError(null)}>
              {error}
            </Alert>
          )}
        </Stack>
      </SettingsSection>
    </>
  );
}

const placementOptions: Array<{ value: NotificationPlacement; label: string }> = [
  { value: 'TOP_LEFT', label: 'Top Left' },
  { value: 'TOP_CENTER', label: 'Top Center' },
  { value: 'TOP_RIGHT', label: 'Top Right' },
  { value: 'BOTTOM_LEFT', label: 'Bottom Left' },
  { value: 'BOTTOM_CENTER', label: 'Bottom Center' },
  { value: 'BOTTOM_RIGHT', label: 'Bottom Right' },
];

const autoDismissOptions: Array<{ value: AutoDismissSeconds; label: string }> = [
  { value: 10, label: '10 seconds' },
  { value: 30, label: '30 seconds' },
  { value: 60, label: '1 minute' },
  { value: 300, label: '5 minutes' },
  { value: 600, label: '10 minutes' },
  { value: 900, label: '15 minutes' },
  { value: 1800, label: '30 minutes' },
];

function NotificationsPage({
  settings,
  onSave,
  onTestNotification,
  onBack,
}: {
  settings: InAppNotificationSettings;
  onSave: (value: InAppNotificationSettings) => Promise<void>;
  onTestNotification: () => void;
  onBack: () => void;
}) {
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const save = async (next: InAppNotificationSettings) => {
    if (saving) return;
    setSaving(true);
    setMessage(null);
    setError(null);
    try {
      await onSave(next);
      setMessage('In-app notification preferences saved.');
    } catch (saveError) {
      setError(saveError instanceof Error ? saveError.message : String(saveError));
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      <SettingsPageHeader
        title="Notifications"
        description="Choose how notification alerts appear inside Lum Harbor."
        onBack={onBack}
        status={<SettingsStatus label="In-app controls" tone="healthy" />}
      />
      <SettingsSection
        id="notifications-in-app"
        title="In-app notifications"
        description="These preferences affect only the alert cards shown inside Lum Harbor."
      >
        <Stack spacing={2.25}>
          <Stack
            direction={{ xs: 'column', sm: 'row' }}
            spacing={2}
            sx={{ alignItems: { sm: 'center' }, justifyContent: 'space-between' }}
          >
            <Box>
              <Typography sx={{ fontWeight: 650 }}>Auto dismiss notifications</Typography>
              <Typography variant="body2" color="text.secondary" sx={{ mt: 0.35 }}>
                Leave off to keep each alert visible until you dismiss it or use its action.
              </Typography>
            </Box>
            <Switch
              checked={settings.autoDismiss}
              disabled={saving}
              onChange={(_event, checked) => void save({ ...settings, autoDismiss: checked })}
              slotProps={{ input: { 'aria-label': 'Auto dismiss notifications' } }}
            />
          </Stack>

          <Collapse in={settings.autoDismiss} unmountOnExit>
            <FormControl size="small" sx={{ minWidth: 220 }}>
              <InputLabel id="notification-duration-label">Dismiss after</InputLabel>
              <Select
                labelId="notification-duration-label"
                label="Dismiss after"
                value={settings.autoDismissSeconds}
                disabled={saving}
                onChange={(event) =>
                  void save({
                    ...settings,
                    autoDismissSeconds: Number(event.target.value) as AutoDismissSeconds,
                  })
                }
              >
                {autoDismissOptions.map((option) => (
                  <MenuItem key={option.value} value={option.value}>
                    {option.label}
                  </MenuItem>
                ))}
              </Select>
            </FormControl>
          </Collapse>

          <Divider />

          <Box>
            <Typography sx={{ fontWeight: 650 }}>Placement</Typography>
            <Typography variant="body2" color="text.secondary" sx={{ mt: 0.35, mb: 1.25 }}>
              New alerts stack inward from the selected edge of the window.
            </Typography>
            <FormControl size="small" sx={{ minWidth: 220 }}>
              <InputLabel id="notification-placement-label">Position</InputLabel>
              <Select
                labelId="notification-placement-label"
                label="Position"
                value={settings.placement}
                disabled={saving}
                onChange={(event) =>
                  void save({ ...settings, placement: event.target.value as NotificationPlacement })
                }
              >
                {placementOptions.map((option) => (
                  <MenuItem key={option.value} value={option.value}>
                    {option.label}
                  </MenuItem>
                ))}
              </Select>
            </FormControl>
          </Box>

          <Divider />

          <Stack
            direction={{ xs: 'column', sm: 'row' }}
            spacing={1.5}
            sx={{ alignItems: { sm: 'center' }, justifyContent: 'space-between' }}
          >
            <Box>
              <Typography sx={{ fontWeight: 650 }}>Preview</Typography>
              <Typography variant="body2" color="text.secondary" sx={{ mt: 0.35 }}>
                Uses the same alert stack as real notifications without creating product data.
              </Typography>
            </Box>
            <Button
              variant="outlined"
              startIcon={<NotificationsNoneRounded />}
              onClick={onTestNotification}
              disabled={saving}
            >
              Test notification
            </Button>
          </Stack>

          {message && (
            <Alert severity="success" variant="outlined" onClose={() => setMessage(null)}>
              {message}
            </Alert>
          )}
          {error && (
            <Alert severity="error" variant="outlined" onClose={() => setError(null)}>
              {error}
            </Alert>
          )}
        </Stack>
      </SettingsSection>
    </>
  );
}

const MACOS_NOTIFICATION_SETTINGS_URL =
  'x-apple.systempreferences:com.apple.Notifications-Settings.extension?bundleID=io.lumharbor.plugin.lh-plugin-macos-notifications';

function NativeNotificationPermissionAlert({
  status,
  onRetry,
}: {
  status?: string;
  onRetry: () => void;
}) {
  const warning = nativeNotificationPermissionWarning(status);
  if (!warning) return null;
  return (
    <Alert
      severity="warning"
      variant="outlined"
      action={
        <Button
          color="inherit"
          size="small"
          onClick={() =>
            warning.action === 'SYSTEM_SETTINGS'
              ? BrowserOpenURL(MACOS_NOTIFICATION_SETTINGS_URL)
              : onRetry()
          }
        >
          {warning.action === 'SYSTEM_SETTINGS' ? 'Open System Settings' : 'Retry'}
        </Button>
      }
    >
      <Typography component="div" sx={{ fontWeight: 650 }}>
        {warning.title}
      </Typography>
      {warning.message}
    </Alert>
  );
}

export default function Settings({
  generalSettings = { closeWindowBehavior: 'background' },
  onSaveGeneralSettings,
  inAppNotificationSettings,
  onSaveInAppNotificationSettings,
  onTestNotification,
}: {
  generalSettings?: GeneralSettings;
  onSaveGeneralSettings?: (value: GeneralSettings) => Promise<void>;
  inAppNotificationSettings: InAppNotificationSettings;
  onSaveInAppNotificationSettings: (value: InAppNotificationSettings) => Promise<void>;
  onTestNotification: () => void;
}) {
  const [view, setView] = useState<SettingsControlPlaneView | null>(null);
  const [page, setPage] = useState<SettingsPageId>('landing');
  const [query, setQuery] = useState('');
  const [highlighted, setHighlighted] = useState<string | null>(null);
  const [detail, setDetail] = useState<DetailSelection>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [installing, setInstalling] = useState(false);
  const [installOutcome, setInstallOutcome] = useState<AddPluginOutcome | null>(null);
  const [addInstanceExtensionId, setAddInstanceExtensionId] = useState<string | null>(null);
  const [creatingInstance, setCreatingInstance] = useState(false);
  const [instanceDialogError, setInstanceDialogError] = useState<string | null>(null);
  const [instanceCreated, setInstanceCreated] = useState<{
    extensionId: string;
    capabilityId: string;
  } | null>(null);
  const [togglingInstanceId, setTogglingInstanceId] = useState<string | null>(null);
  const [toggleError, setToggleError] = useState<string | null>(null);
  const [toggleNotice, setToggleNotice] = useState<{
    instanceId: string;
    enabled: boolean;
  } | null>(null);
  const [removeConfirmExtensionId, setRemoveConfirmExtensionId] = useState<string | null>(null);
  const [removingPlugin, setRemovingPlugin] = useState(false);
  const [removeDialogError, setRemoveDialogError] = useState<string | null>(null);
  const [pluginRemoved, setPluginRemoved] = useState<{ extensionId: string } | null>(null);
  const [editInstanceTarget, setEditInstanceTarget] = useState<{
    extension: ExtensionView;
    instance: ExtensionView['instances'][number];
  } | null>(null);
  const [updatingInstance, setUpdatingInstance] = useState(false);
  const [editInstanceDialogError, setEditInstanceDialogError] = useState<string | null>(null);
  const [instanceUpdated, setInstanceUpdated] = useState<{
    instanceId: string;
  } | null>(null);

  const handleUpdateInstance = async (instanceId: string, document: SettingsConfigDocument) => {
    if (updatingInstance) return;
    setUpdatingInstance(true);
    try {
      const outcome: UpdateInstanceConfigOutcome = await updatePluginInstanceConfig(
        pluginManagementActions,
        {
          instanceId,
          config: document,
        },
      );
      if (outcome.status === 'updated') {
        setEditInstanceDialogError(null);
        setInstanceUpdated({ instanceId: outcome.result.instanceId });
        setEditInstanceTarget(null);
        void loadView();
      } else {
        setEditInstanceDialogError(outcome.message);
      }
    } finally {
      setUpdatingInstance(false);
    }
  };

  const handleAddPlugin = async () => {
    if (installing) return;
    setInstalling(true);
    try {
      const outcome = await addPlugin(pluginManagementActions);
      if (outcome.status !== 'canceled') {
        setInstallOutcome(outcome);
      }
    } finally {
      setInstalling(false);
    }
  };

  const handleCreateInstance = async (capabilityId: string, document: SettingsConfigDocument) => {
    if (!addInstanceExtensionId || creatingInstance) return;
    setCreatingInstance(true);
    try {
      const outcome: CreateInstanceOutcome = await createPluginInstance(pluginManagementActions, {
        extensionId: addInstanceExtensionId,
        capabilityId,
        config: document,
      });
      if (outcome.status === 'created') {
        setInstanceDialogError(null);
        setInstanceCreated({
          extensionId: outcome.result.extensionId,
          capabilityId: outcome.result.capabilityId,
        });
        setAddInstanceExtensionId(null);
        // Desired declarations changed; runtime facts stay current-lifetime.
        void loadView();
      } else {
        setInstanceDialogError(outcome.message);
      }
    } finally {
      setCreatingInstance(false);
    }
  };

  const handleToggleInstance = async (instanceId: string, enabled: boolean) => {
    if (togglingInstanceId) return;
    setTogglingInstanceId(instanceId);
    setToggleError(null);
    try {
      const outcome: SetInstanceEnabledOutcome = await setPluginInstanceEnabled(
        pluginManagementActions,
        instanceId,
        enabled,
      );
      if (outcome.status === 'updated') {
        setToggleNotice({ instanceId: outcome.result.instanceId, enabled: outcome.result.enabled });
        void loadView();
      } else {
        setToggleError(outcome.message);
      }
    } finally {
      setTogglingInstanceId(null);
    }
  };

  const handleRemovePlugin = async () => {
    if (!removeConfirmExtensionId || removingPlugin) return;
    setRemovingPlugin(true);
    try {
      const outcome: RemovePluginOutcome = await removePlugin(
        pluginManagementActions,
        removeConfirmExtensionId,
      );
      if (outcome.status === 'removed') {
        setRemoveDialogError(null);
        setPluginRemoved({ extensionId: outcome.result.extensionId });
        setRemoveConfirmExtensionId(null);
        setDetail(null);
        void loadView();
      } else {
        setRemoveDialogError(outcome.message);
      }
    } finally {
      setRemovingPlugin(false);
    }
  };

  const loadView = () =>
    settingsDataSource
      .load()
      .then(setView)
      .catch((error: unknown) => {
        setLoadError(error instanceof Error ? error.message : String(error));
      });

  useEffect(() => {
    void loadView();
  }, []);

  useEffect(() => {
    if (!highlighted) return;
    const frame = requestAnimationFrame(() => {
      const target = document.getElementById(highlighted);
      target?.scrollIntoView({ behavior: 'smooth', block: 'start' });
      target?.focus({ preventScroll: true });
    });
    const timeout = window.setTimeout(() => setHighlighted(null), 1800);
    return () => {
      cancelAnimationFrame(frame);
      window.clearTimeout(timeout);
    };
  }, [highlighted, page]);

  const searchResults = useMemo(
    () => (view ? searchSettings(view.searchEntries, query) : []),
    [query, view],
  );

  if (loadError) {
    return (
      <PageContainer>
        <Alert severity="error" variant="outlined">
          Plugin inventory could not be loaded: {loadError}
        </Alert>
      </PageContainer>
    );
  }

  if (!view) {
    return (
      <PageContainer>
        <Stack sx={{ minHeight: 300, alignItems: 'center', justifyContent: 'center' }}>
          <CircularProgress size={24} />
        </Stack>
      </PageContainer>
    );
  }

  const goBack = () => {
    setPage('landing');
    setHighlighted(null);
    setDetail(null);
  };
  const openSearchResult = (entry: SettingsSearchEntry) => {
    setPage(entry.page);
    setQuery('');
    setHighlighted(entry.targetId ?? null);
    if (entry.detailId) {
      const kind =
        entry.page === 'extensions'
          ? 'extension'
          : entry.page === 'integrations'
            ? 'integration'
            : null;
      if (kind) setDetail({ kind, id: entry.detailId });
    }
  };

  const middleware =
    detail?.kind === 'middleware'
      ? (view.ai.middleware.find((item) => item.id === detail.id) ?? null)
      : null;
  const integration =
    detail?.kind === 'integration'
      ? (view.integrations.connected.find((item) => item.id === detail.id) ?? null)
      : null;
  const extension =
    detail?.kind === 'extension'
      ? (view.extensions.find((item) => item.id === detail.id) ?? null)
      : null;
  const addInstanceExtension = addInstanceExtensionId
    ? (view.extensions.find((item) => item.id === addInstanceExtensionId) ?? null)
    : null;
  const removeConfirmExtension = removeConfirmExtensionId
    ? (view.extensions.find((item) => item.id === removeConfirmExtensionId) ?? null)
    : null;
  return (
    <PageContainer>
      {page === 'landing' && (
        <LandingPage
          view={view}
          query={query}
          results={searchResults}
          onQueryChange={setQuery}
          onOpenPage={setPage}
          onSelectResult={openSearchResult}
        />
      )}
      {page === 'ai' && (
        <AIPage
          view={view.ai}
          platform={view.platform}
          highlighted={highlighted}
          onBack={goBack}
          onOpenMiddleware={(id) => setDetail({ kind: 'middleware', id })}
          onOpenExtension={(id) => setDetail({ kind: 'extension', id })}
        />
      )}
      {page === 'integrations' && (
        <IntegrationsPage
          view={view.integrations}
          highlighted={highlighted}
          onBack={goBack}
          onOpen={(id) => setDetail({ kind: 'integration', id })}
        />
      )}
      {page === 'extensions' && (
        <ExtensionsPage
          extensions={view.extensions}
          highlighted={highlighted}
          onBack={goBack}
          onOpen={(id) => setDetail({ kind: 'extension', id })}
          installing={installing}
          installOutcome={installOutcome}
          onAddPlugin={() => void handleAddPlugin()}
          onDismissInstallNotice={() => setInstallOutcome(null)}
          instanceCreated={instanceCreated}
          onDismissInstanceNotice={() => setInstanceCreated(null)}
          pluginRemoved={pluginRemoved}
          onDismissRemovedNotice={() => setPluginRemoved(null)}
        />
      )}
      {page === 'system' && (
        <SystemPage view={view.system} highlighted={highlighted} onBack={goBack} />
      )}
      {page === 'general' && (
        <GeneralPage settings={generalSettings} onSave={onSaveGeneralSettings} onBack={goBack} />
      )}
      {page === 'notifications' && (
        <NotificationsPage
          settings={inAppNotificationSettings}
          onSave={onSaveInAppNotificationSettings}
          onTestNotification={onTestNotification}
          onBack={goBack}
        />
      )}

      <SettingsDetailDrawer
        open={Boolean(middleware)}
        title={middleware?.name ?? ''}
        subtitle={middleware?.summary}
        onClose={() => setDetail(null)}
      >
        {middleware && <MiddlewareDetail middleware={middleware} />}
      </SettingsDetailDrawer>
      <SettingsDetailDrawer
        open={Boolean(integration)}
        title={integration ? `${integration.typeName} · ${integration.instanceName}` : ''}
        subtitle={integration?.detail}
        onClose={() => setDetail(null)}
      >
        {integration && <IntegrationDetail integration={integration} />}
      </SettingsDetailDrawer>
      <SettingsDetailDrawer
        open={Boolean(extension)}
        title={extension?.displayName ?? ''}
        subtitle={extension?.summary}
        onClose={() => setDetail(null)}
      >
        {extension && (
          <Stack spacing={3}>
            <Box>
              <Typography variant="h2">Overview</Typography>
              <Stack direction="row" spacing={2} useFlexGap sx={{ mt: 1, flexWrap: 'wrap' }}>
                <Typography color="text.secondary">Version {extension.version}</Typography>
                <SettingsStatus label="Installed" tone="neutral" />
                <SettingsStatus
                  label={extension.selected ? 'Selected' : 'Not selected'}
                  tone={extension.selected ? 'healthy' : 'inactive'}
                />
                <SettingsStatus
                  label={extension.running ? 'Process running' : 'Process stopped'}
                  tone={extension.running ? 'healthy' : 'inactive'}
                />
              </Stack>
            </Box>
            <Divider />
            <Box>
              <Typography variant="h2">Capabilities</Typography>
              <Stack direction="row" spacing={0.75} useFlexGap sx={{ mt: 1, flexWrap: 'wrap' }}>
                {extension.capabilities.map((capability) => (
                  <Chip
                    key={capability}
                    size="small"
                    variant="outlined"
                    label={getCapabilityDisplayLabel(capability)}
                  />
                ))}
              </Stack>
              <Stack spacing={0.5} sx={{ mt: 1.5 }}>
                {extension.bindings.map((binding) => (
                  <Typography key={binding} color="text.secondary">
                    • {binding}
                  </Typography>
                ))}
              </Stack>
            </Box>
            <Divider />
            <Box>
              <Typography variant="h2">Configured instances</Typography>
              {extension.fields.some((f) => f.type === 'secret') && (
                <Alert severity="info" variant="outlined" sx={{ mt: 1.25 }}>
                  This plugin uses credentials that cannot be stored in configuration. They are
                  externally managed via environment variables (such as LUM_HARBOR_OPENAI_API_KEY)
                  or optional for local runtimes.
                </Alert>
              )}
              {instanceUpdated &&
                extension.instances.some(
                  (instance) => instance.id === instanceUpdated.instanceId,
                ) && (
                  <Alert
                    severity="success"
                    variant="outlined"
                    sx={{ mt: 1.25 }}
                    onClose={() => setInstanceUpdated(null)}
                  >
                    Configuration saved. Restart Lum Harbor for changes to take effect.
                  </Alert>
                )}
              {toggleNotice &&
                extension.instances.some((instance) => instance.id === toggleNotice.instanceId) && (
                  <Alert
                    severity="success"
                    variant="outlined"
                    sx={{ mt: 1.25 }}
                    onClose={() => setToggleNotice(null)}
                  >
                    {toggleNotice.enabled
                      ? 'Enabled again. Restart Lum Harbor to start using this Plugin instance.'
                      : 'Stopped using. Restart Lum Harbor for this to take effect.'}
                  </Alert>
                )}
              {toggleError && (
                <Alert
                  severity="error"
                  variant="outlined"
                  sx={{ mt: 1.25 }}
                  onClose={() => setToggleError(null)}
                >
                  {toggleError}
                </Alert>
              )}
              <Stack spacing={1} sx={{ mt: 1.25 }}>
                {extension.instances.map((instance) => (
                  <Paper
                    key={instance.id}
                    variant="outlined"
                    sx={{
                      p: 1.25,
                    }}
                  >
                    <Stack spacing={1.25}>
                      <Box
                        sx={{
                          display: 'flex',
                          alignItems: 'center',
                          justifyContent: 'space-between',
                          gap: 1.5,
                        }}
                      >
                        <Box sx={{ minWidth: 0 }}>
                          <Stack
                            direction="row"
                            spacing={1}
                            sx={{ alignItems: 'center', flexWrap: 'wrap' }}
                          >
                            <Typography sx={{ fontWeight: 650 }}>{instance.id}</Typography>
                            <SettingsStatus
                              label={instance.enabled ? 'Enabled' : 'Disabled'}
                              tone={instance.enabled ? 'healthy' : 'inactive'}
                            />
                          </Stack>
                          <Typography variant="caption" color="text.secondary">
                            {instance.capabilityId}
                          </Typography>
                        </Box>
                        <Stack direction="row" spacing={1}>
                          {extension.fields.filter((field) => field.type !== 'secret').length >
                            0 && (
                            <Button
                              variant="outlined"
                              size="small"
                              disabled={togglingInstanceId !== null}
                              onClick={() => {
                                setEditInstanceDialogError(null);
                                setEditInstanceTarget({ extension, instance });
                              }}
                            >
                              Configure
                            </Button>
                          )}
                          <Button
                            variant="outlined"
                            size="small"
                            disabled={togglingInstanceId !== null}
                            startIcon={
                              togglingInstanceId === instance.id ? (
                                <CircularProgress size={14} color="inherit" />
                              ) : undefined
                            }
                            onClick={() =>
                              void handleToggleInstance(instance.id, !instance.enabled)
                            }
                          >
                            {instance.enabled ? 'Stop Using' : 'Use Again'}
                          </Button>
                        </Stack>
                      </Box>
                      {instance.enabled && instance.capabilityId === 'notification.deliver' && (
                        <NativeNotificationPermissionAlert
                          status={instance.nativeNotificationAuthorization}
                          onRetry={() => void loadView()}
                        />
                      )}
                    </Stack>
                  </Paper>
                ))}
                {extension.instances.length === 0 && (
                  <Typography color="text.secondary">
                    This Plugin is installed but has no configured instances.
                  </Typography>
                )}
              </Stack>
              {extension.selectableCapabilities.length > 0 && (
                <Button
                  variant="outlined"
                  size="small"
                  sx={{ mt: 1.5 }}
                  onClick={() => setAddInstanceExtensionId(extension.id)}
                >
                  Add Instance
                </Button>
              )}
            </Box>
            <Divider />
            <Stack spacing={1}>
              <Accordion
                disableGutters
                defaultExpanded={DEFAULT_EXTENSION_DISCLOSURE_STATE.developer}
                elevation={0}
                sx={{ border: 1, borderColor: 'divider', '&:before': { display: 'none' } }}
              >
                <AccordionSummary
                  expandIcon={<ExpandMoreRounded />}
                  aria-controls={`${extension.id}-developer-content`}
                  id={`${extension.id}-developer-header`}
                >
                  <Box>
                    <Typography sx={{ fontWeight: 650 }}>Developer</Typography>
                    <Typography variant="caption" color="text.secondary">
                      IDs and protocol details
                    </Typography>
                  </Box>
                </AccordionSummary>
                <AccordionDetails id={`${extension.id}-developer-content`}>
                  <DeveloperDetails extension={extension} />
                </AccordionDetails>
              </Accordion>
            </Stack>
            <Divider />
            <Box>
              <Typography variant="h2">Remove Plugin</Typography>
              <Typography color="text.secondary" sx={{ mt: 0.75 }}>
                {extension.selected
                  ? 'Stop using every instance of this Plugin, then restart Lum Harbor, before it can be removed.'
                  : extension.running
                    ? "This Plugin's process is still running from this session. Restart Lum Harbor before removing it."
                    : 'This Plugin has no enabled instances and is not running. It can be removed.'}
              </Typography>
              <Button
                variant="outlined"
                color="error"
                size="small"
                sx={{ mt: 1.5 }}
                disabled={!canRemovePlugin(extension)}
                onClick={() => setRemoveConfirmExtensionId(extension.id)}
              >
                Remove Plugin
              </Button>
            </Box>
          </Stack>
        )}
      </SettingsDetailDrawer>
      <AddInstanceDialog
        extension={addInstanceExtension}
        open={Boolean(addInstanceExtensionId)}
        submitting={creatingInstance}
        error={instanceDialogError}
        onClose={() => {
          setAddInstanceExtensionId(null);
          setInstanceDialogError(null);
        }}
        onSubmit={(capabilityId, document) => void handleCreateInstance(capabilityId, document)}
      />
      <EditInstanceDialog
        target={editInstanceTarget}
        open={Boolean(editInstanceTarget)}
        submitting={updatingInstance}
        error={editInstanceDialogError}
        onClose={() => {
          setEditInstanceTarget(null);
          setEditInstanceDialogError(null);
        }}
        onSubmit={(instanceId, document) => void handleUpdateInstance(instanceId, document)}
      />
      <RemovePluginDialog
        extension={removeConfirmExtension}
        open={Boolean(removeConfirmExtensionId)}
        submitting={removingPlugin}
        error={removeDialogError}
        onClose={() => {
          setRemoveConfirmExtensionId(null);
          setRemoveDialogError(null);
        }}
        onConfirm={() => void handleRemovePlugin()}
      />
    </PageContainer>
  );
}

function MiddlewareDetail({ middleware }: { middleware: AIMiddlewareBindingView }) {
  return (
    <Stack spacing={2.5}>
      <Stack direction="row" sx={{ justifyContent: 'space-between' }}>
        <Typography color="text.secondary">Stage</Typography>
        <Chip
          size="small"
          variant="outlined"
          label={getPipelineStageDisplayLabel(middleware.stage)}
        />
      </Stack>
      <Stack direction="row" sx={{ justifyContent: 'space-between' }}>
        <Typography color="text.secondary">State</Typography>
        <SettingsStatus
          label={middleware.enabled ? 'Enabled' : 'Disabled'}
          tone={middleware.enabled ? 'healthy' : 'inactive'}
        />
      </Stack>
      <Box>
        <Typography variant="h2">Provided by extension</Typography>
        <Typography color="text.secondary" sx={{ mt: 0.6 }}>
          {middleware.extensionName}
        </Typography>
      </Box>
      <Box>
        <Typography variant="h2">Used for</Typography>
        <Stack direction="row" spacing={0.75} useFlexGap sx={{ mt: 1, flexWrap: 'wrap' }}>
          {(middleware.actions === 'ALL' ? ['All actions'] : middleware.actions).map((action) => (
            <Chip
              key={action}
              size="small"
              label={action === 'All actions' ? 'All AI features' : getActionDisplayLabel(action)}
              variant="outlined"
            />
          ))}
        </Stack>
      </Box>
      <Button variant="outlined" disabled>
        Open extension configuration
      </Button>
    </Stack>
  );
}

function IntegrationDetail({ integration }: { integration: IntegrationInstanceView }) {
  return (
    <Stack spacing={2.5}>
      <Stack direction="row" sx={{ justifyContent: 'space-between' }}>
        <Typography color="text.secondary">Health</Typography>
        <SettingsStatus label={integration.status} tone={integration.health} />
      </Stack>
      <Box>
        <Typography variant="h2">Connection</Typography>
        <Typography color="text.secondary" sx={{ mt: 0.6 }}>
          {integration.typeName} · {integration.instanceName}
        </Typography>
      </Box>
      <Box>
        <Typography variant="h2">Provided by extension</Typography>
        <Typography color="text.secondary" sx={{ mt: 0.6 }}>
          {integration.extensionName}
        </Typography>
      </Box>
      {integration.lastActivity && (
        <Box>
          <Typography variant="h2">Activity</Typography>
          <Typography color="text.secondary" sx={{ mt: 0.6 }}>
            {integration.lastActivity}
          </Typography>
        </Box>
      )}
      <Alert severity="info" variant="outlined">
        This is the current startup configuration. Instance changes are not available yet.
      </Alert>
    </Stack>
  );
}

function DeveloperDetails({ extension }: { extension: ExtensionView }) {
  return (
    <Stack spacing={2}>
      <Box>
        <Typography variant="caption" color="text.secondary">
          Extension ID
        </Typography>
        <Typography sx={{ mt: 0.35, fontFamily: 'monospace', fontSize: 12 }}>
          {extension.developer.packageId}
        </Typography>
      </Box>
      <Box>
        <Typography variant="caption" color="text.secondary">
          Canonical capability IDs
        </Typography>
        <Stack direction="row" spacing={0.75} useFlexGap sx={{ mt: 0.75, flexWrap: 'wrap' }}>
          {extension.capabilities.map((capability) => (
            <Chip key={capability} size="small" label={capability} variant="outlined" />
          ))}
        </Stack>
      </Box>
      <Box>
        <Typography variant="caption" color="text.secondary">
          Protocol
        </Typography>
        <Typography sx={{ mt: 0.35 }}>{extension.developer.protocol}</Typography>
      </Box>
    </Stack>
  );
}
