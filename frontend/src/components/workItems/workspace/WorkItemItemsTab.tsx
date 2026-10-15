import SearchRounded from '@mui/icons-material/SearchRounded';
import { useEffect, useMemo, useRef, useState } from 'react';
import {
  Box,
  InputAdornment,
  MenuItem,
  Paper,
  Select,
  Stack,
  TextField,
  ToggleButton,
  ToggleButtonGroup,
  Typography,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { getHealthColor } from '../workflow/workflowLayout.ts';
import { WORKFLOW_STAGES, type WorkflowModel } from '../workflow/workflowTypes.ts';
import WorkItemInspector from './WorkItemInspector.tsx';
import {
  filterItemExplorerNodes,
  getItemListRowPresentation,
  resolveItemExplorerSelection,
  type ItemExplorerRiskFilter,
  type ItemExplorerStageFilter,
} from './itemExplorer.ts';

interface WorkItemItemsTabProps {
  model: WorkflowModel;
  selectedNodeId?: string | null;
  onSelectNode?: (nodeId: string) => void;
}

export default function WorkItemItemsTab({
  model,
  selectedNodeId = null,
  onSelectNode,
}: WorkItemItemsTabProps) {
  const theme = useTheme();
  const [query, setQuery] = useState('');
  const [risk, setRisk] = useState<ItemExplorerRiskFilter>('all');
  const [stage, setStage] = useState<ItemExplorerStageFilter>('all');
  const rowRefs = useRef(new Map<string, HTMLButtonElement>());
  const resolvedSelectedId = resolveItemExplorerSelection(model, selectedNodeId);
  const selectedNode = model.nodes.find((node) => node.id === resolvedSelectedId) ?? null;
  const visibleNodes = useMemo(
    () => filterItemExplorerNodes(model, query, risk, stage),
    [model, query, risk, stage],
  );

  const selectItem = (nodeId: string) => {
    if (!visibleNodes.some((node) => node.id === nodeId)) {
      setQuery('');
      setRisk('all');
      setStage('all');
    }
    onSelectNode?.(nodeId);
  };

  useEffect(() => {
    if (!selectedNodeId && resolvedSelectedId) onSelectNode?.(resolvedSelectedId);
  }, [onSelectNode, resolvedSelectedId, selectedNodeId]);

  useEffect(() => {
    if (resolvedSelectedId)
      rowRefs.current.get(resolvedSelectedId)?.scrollIntoView({ block: 'nearest' });
  }, [resolvedSelectedId, visibleNodes]);

  if (model.nodes.length === 0) {
    return (
      <Paper variant="outlined" sx={{ p: 4.5, textAlign: 'center' }}>
        <Typography variant="h3">No items connected</Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
          Authoritative child-item relationships have not been connected to this workflow.
        </Typography>
      </Paper>
    );
  }

  return (
    <Stack spacing={1.25}>
      <Box>
        <Typography variant="h2">Item Explorer</Typography>
        <Typography variant="caption" color="text.secondary">
          Investigate item state, dependency chains, related work, evidence, and activity.
        </Typography>
      </Box>

      <Paper variant="outlined" sx={{ overflow: 'hidden' }}>
        <Box
          sx={{
            display: 'grid',
            gridTemplateColumns: { xs: '1fr', lg: 'minmax(260px, 1fr) minmax(0, 2.1fr)' },
            minHeight: 560,
          }}
        >
          <Box
            sx={{
              minWidth: 0,
              borderRight: { lg: 1 },
              borderBottom: { xs: 1, lg: 0 },
              borderColor: 'divider',
            }}
          >
            <Stack
              spacing={0.75}
              sx={{ p: 1, bgcolor: 'surface.subtle', borderBottom: 1, borderColor: 'divider' }}
            >
              <TextField
                size="small"
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder="Search items…"
                slotProps={{
                  htmlInput: { 'aria-label': 'Search items' },
                  input: {
                    startAdornment: (
                      <InputAdornment position="start">
                        <SearchRounded sx={{ fontSize: 17 }} />
                      </InputAdornment>
                    ),
                  },
                }}
              />
              <Stack
                direction="row"
                spacing={1}
                sx={{ justifyContent: 'space-between', alignItems: 'center' }}
              >
                <ToggleButtonGroup
                  exclusive
                  size="small"
                  value={risk}
                  onChange={(_, value) => value && setRisk(value)}
                  aria-label="Item health filter"
                >
                  <ToggleButton value="all">All</ToggleButton>
                  <ToggleButton value="at-risk">At Risk</ToggleButton>
                  <ToggleButton value="blocked">Blocked</ToggleButton>
                </ToggleButtonGroup>
                <Select
                  size="small"
                  value={stage}
                  onChange={(event) => setStage(event.target.value as ItemExplorerStageFilter)}
                  aria-label="Stage filter"
                  sx={{ minWidth: 112 }}
                >
                  <MenuItem value="all">All stages</MenuItem>
                  {WORKFLOW_STAGES.map((entry) => (
                    <MenuItem key={entry.id} value={entry.id}>
                      {entry.label}
                    </MenuItem>
                  ))}
                </Select>
              </Stack>
            </Stack>

            <Box sx={{ maxHeight: { lg: 720 }, overflowY: { lg: 'auto' } }}>
              {visibleNodes.length === 0 ? (
                <Box sx={{ p: 3, textAlign: 'center' }}>
                  <Typography variant="body2" color="text.secondary">
                    No items match these filters.
                  </Typography>
                </Box>
              ) : (
                visibleNodes.map((node) => {
                  const selected = node.id === resolvedSelectedId;
                  const row = getItemListRowPresentation(node, model);
                  const healthColor = getHealthColor(node.health, theme.palette.mode === 'dark');
                  return (
                    <Box
                      key={node.id}
                      component="button"
                      ref={(element: HTMLButtonElement | null) => {
                        if (element) rowRefs.current.set(node.id, element);
                        else rowRefs.current.delete(node.id);
                      }}
                      onClick={() => selectItem(node.id)}
                      aria-pressed={selected}
                      aria-label={row.accessibilityLabel}
                      title={row.tooltip}
                      sx={{
                        appearance: 'none',
                        width: '100%',
                        px: 1.5,
                        py: 0.78,
                        minHeight: 48,
                        textAlign: 'left',
                        color: 'text.primary',
                        bgcolor: selected ? 'action.selected' : 'transparent',
                        border: 0,
                        borderBottom: 1,
                        borderLeft: 3,
                        borderColor: 'divider',
                        borderLeftColor: selected ? 'primary.main' : 'transparent',
                        cursor: 'pointer',
                        '&:hover': { bgcolor: 'action.hover' },
                      }}
                    >
                      <Stack
                        direction="row"
                        spacing={0.9}
                        sx={{ alignItems: 'center', minWidth: 0 }}
                      >
                        <Box
                          aria-hidden
                          sx={{
                            width: 17,
                            flex: '0 0 17px',
                            color: healthColor,
                            fontFamily: 'monospace',
                            fontSize: 17,
                            fontWeight: 700,
                            lineHeight: 1,
                            textAlign: 'center',
                          }}
                        >
                          {row.shapeSymbol}
                        </Box>
                        <Typography
                          variant="body2"
                          noWrap
                          sx={{ minWidth: 0, fontWeight: selected ? 700 : 650, flex: 1 }}
                        >
                          {row.title}
                        </Typography>
                      </Stack>
                      <Typography
                        variant="caption"
                        color="text.secondary"
                        noWrap
                        sx={{ display: 'block', mt: 0.3, ml: 3.25 }}
                      >
                        {row.secondaryText}
                      </Typography>
                    </Box>
                  );
                })
              )}
            </Box>
          </Box>

          <Box sx={{ minWidth: 0 }}>
            <Box
              sx={{
                px: 2,
                py: 1,
                bgcolor: 'surface.subtle',
                borderBottom: 1,
                borderColor: 'divider',
              }}
            >
              <Typography variant="caption" color="text.secondary" sx={{ fontWeight: 700 }}>
                SELECTED ITEM DETAIL
              </Typography>
            </Box>
            <WorkItemInspector
              node={selectedNode}
              model={model}
              mode="detail"
              embedded
              onClearSelection={() => undefined}
              onSelectNode={selectItem}
            />
          </Box>
        </Box>
      </Paper>
    </Stack>
  );
}
