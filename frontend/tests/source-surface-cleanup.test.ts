import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import test from 'node:test';
import { fileURLToPath } from 'node:url';
import {
  proposalOrigin,
  proposalOriginLabel,
  proposalProvenanceLabel,
  sourceOriginRowLabel,
} from '../src/proposals/proposalOrigin.ts';

const srcRoot = fileURLToPath(new URL('../src/', import.meta.url));

function productionUIFiles(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = `${directory}/${entry.name}`;
    if (entry.isDirectory()) return productionUIFiles(path);
    return /\.(ts|tsx)$/.test(entry.name) ? [path] : [];
  });
}

test('Assistant has one conversational mode and no explicit Source-ingestion mode', () => {
  const panel = readFileSync(`${srcRoot}/components/AssistantPanel.tsx`, 'utf8');
  assert.doesNotMatch(panel, /LocalSourceInbox|activeTab|<Tabs|<Tab/);
  assert.match(panel, /props\.onSend\(request\)/);
  assert.match(panel, /Ask Lum Harbor/);
});

test('production UI does not expose Local Source as a product term', () => {
  for (const path of productionUIFiles(srcRoot)) {
    const contents = readFileSync(path, 'utf8');
    assert.doesNotMatch(contents, /local[ -]sources?/i, path);
  }
});

test('non-Assistant Source provenance uses the universal Source label', () => {
  const fromCoreProducer = {
    aiRunId: 'run-1',
    sourceRevisionIds: ['rev-core'],
    sourceExtensionIds: ['core.local'],
  };
  assert.equal(proposalOrigin(fromCoreProducer), 'source');
  assert.equal(sourceOriginRowLabel(fromCoreProducer), 'From Source');
  assert.equal(proposalOriginLabel('source'), 'From Source');
  assert.equal(
    proposalProvenanceLabel({
      ...fromCoreProducer,
      sourceProvenances: [
        {
          extensionId: 'core.local',
          extensionName: 'Source',
          externalId: 'note-1',
          sourceKind: 'NOTE',
        },
      ],
    }),
    'From Source',
  );

  const fromAssistant = {
    aiRunId: 'run-2',
    sourceRevisionIds: ['rev-turn'],
    sourceExtensionIds: ['core.assistant'],
  };
  assert.equal(proposalOrigin(fromAssistant), 'assistant');
  assert.equal(sourceOriginRowLabel(fromAssistant), null);
});
