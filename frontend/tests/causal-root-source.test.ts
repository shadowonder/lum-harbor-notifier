import assert from 'node:assert/strict';
import test from 'node:test';
import { rootSourceLabel, rootSourceText } from '../src/components/history/rootSourceText.ts';

test('causal root Source shows the captured user input, not the envelope', () => {
  assert.equal(
    rootSourceText({
      payloadJson:
        '{"message":"Propose creating a todo next Friday at 3 PM.","surface":"DASHBOARD","workItemId":""}',
    }),
    'Propose creating a todo next Friday at 3 PM.',
  );
  assert.equal(
    rootSourceText({ payloadJson: '{"title":"Manual Verification WorkItem"}' }),
    'Manual Verification WorkItem',
  );
  assert.equal(rootSourceText({ payloadJson: '{"a":1}' }), '{"a":1}');
  assert.equal(rootSourceText({ payloadJson: 'plain note' }), 'plain note');
  assert.ok(
    rootSourceText({ payloadJson: JSON.stringify({ text: 'x'.repeat(700) }) }).endsWith('…'),
  );
  assert.equal(
    rootSourceLabel({
      extensionId: 'core.assistant',
      instanceId: 'direct-turn',
      sourceKind: 'USER_INTENT',
    }),
    'core.assistant / direct-turn · USER_INTENT',
  );
});
