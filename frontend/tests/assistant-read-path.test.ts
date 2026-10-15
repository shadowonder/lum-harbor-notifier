import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import {
  assistantKindLabel,
  locatorForWorkItem,
  sendAssistantMessage,
} from '../src/assistant/assistantConversation.ts';
import { mapAssistantResult, type AssistantApi } from '../src/api/assistant/assistantApi.ts';

const source = (path: string) => readFileSync(new URL(`../src/${path}`, import.meta.url), 'utf8');

function fakeApi(result: Awaited<ReturnType<AssistantApi['ask']>> | Error) {
  const calls: Array<{ message: string; context: unknown }> = [];
  const api: AssistantApi = {
    async ask(message, context) {
      calls.push({ message, context });
      if (result instanceof Error) throw result;
      return result;
    },
  };
  return { api, calls };
}

test('selected WorkItem identity becomes the locator; no selection stays context-free', () => {
  assert.deepEqual(locatorForWorkItem('wi-1'), { surface: 'WORK_ITEMS', workItemId: 'wi-1' });
  assert.deepEqual(locatorForWorkItem(null), { surface: '', workItemId: '' });
});

test('the locator carries identity only, never WorkItem facts', () => {
  assert.deepEqual(Object.keys(locatorForWorkItem('wi-1')).sort(), ['surface', 'workItemId']);
});

test('send produces user + assistant entries and passes the selected identity', async () => {
  const { api, calls } = fakeApi({
    kind: 'ANSWER',
    text: 'It is a release.',
    contextLabel: 'Q4',
    changeApplied: false,
    proposalOutcome: '',
    proposalId: '',
  });
  const entries = await sendAssistantMessage(api, '  Summarize this work.  ', 'wi-1');
  assert.equal(entries.length, 2);
  assert.equal(entries[0].role, 'user');
  assert.equal(entries[1].role, 'assistant');
  assert.deepEqual(calls[0], {
    message: 'Summarize this work.',
    context: { surface: 'WORK_ITEMS', workItemId: 'wi-1' },
  });
});

test('mutation intent is shown as unapplied, never as success', async () => {
  const { api } = fakeApi({
    kind: 'MUTATION_INTENT',
    text: 'Clarify the description.',
    contextLabel: 'Q4',
    changeApplied: false,
    proposalOutcome: '',
    proposalId: '',
  });
  const [, reply] = await sendAssistantMessage(api, 'Change the description', 'wi-1');
  assert.equal(reply.role === 'assistant' && reply.changeApplied, false);
  assert.match(assistantKindLabel('MUTATION_INTENT') ?? '', /nothing was changed/);
});

test('backend failure becomes an honest error entry with no demo fallback answer', async () => {
  const { api } = fakeApi(new Error('The AI runtime could not answer.'));
  const entries = await sendAssistantMessage(api, 'hi', 'wi-1');
  assert.equal(entries[1].role, 'error');
  assert.equal(entries[1].text, 'The AI runtime could not answer.');
});

test('blank messages send nothing', async () => {
  const { api, calls } = fakeApi(new Error('unused'));
  assert.deepEqual(await sendAssistantMessage(api, '   ', 'wi-1'), []);
  assert.equal(calls.length, 0);
});

test('unknown result kinds are rejected instead of coerced', () => {
  assert.throws(() =>
    mapAssistantResult({ kind: 'CHAT', text: 'x', contextLabel: '', changeApplied: false }),
  );
});

test('AssistantPanel is a real caller: no regex demo answer or fake proposal builder remains', () => {
  const panel = source('components/AssistantPanel.tsx');
  const shell = source('components/AppShell.tsx');
  assert.equal(panel.includes('No data was changed'), false);
  assert.equal(/move\|change\|update/.test(panel), false);
  assert.equal(panel.includes('onCreateForeground'), false);
  assert.equal(shell.includes('createForegroundProposal'), false);
  assert.equal(shell.includes("workItemId: 'production-release'"), false);
  assert.match(panel, /props\.onSend\(request\)/);
});

test('selected WorkItem reaches the Assistant through AppShell and WorkItems', () => {
  const shell = source('components/AppShell.tsx');
  const page = source('pages/WorkItems.tsx');
  assert.match(page, /onSelectedWorkItemChange\?\.\(selectedId \? \{ id: selectedId/);
  assert.match(shell, /sendAssistantMessage\(\s*assistantApi,\s*message,\s*assistantWorkItemId,/);
});

test('Assistant placement and design contract is unchanged', () => {
  const shell = source('components/AppShell.tsx');
  const panel = source('components/AssistantPanel.tsx');
  assert.match(shell, /400px/);
  assert.match(shell, /<Drawer/);
  assert.match(panel, /aria-label="AI Assistant"/);
  assert.match(panel, /Ask Lum Harbor/);
});

test('React never imports or constructs provider SDK types for the Assistant', () => {
  for (const path of [
    'api/assistant/assistantApi.ts',
    'api/assistant/wailsAssistantApi.ts',
    'assistant/assistantConversation.ts',
    'components/AssistantPanel.tsx',
  ]) {
    const text = source(path);
    assert.equal(/openai|anthropic|api\.openai\.com/i.test(text), false, path);
    assert.equal(text.includes('wailsjs/go/models'), false, path);
  }
});
