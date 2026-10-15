import { AskAssistant } from '../../../wailsjs/go/app/App';
import { mapAssistantResult, type AssistantApi } from './assistantApi.ts';

export const wailsAssistantApi: AssistantApi = {
  async ask(message, context) {
    const result = await AskAssistant({
      message,
      surface: context.surface,
      workItemId: context.workItemId,
    });
    return mapAssistantResult(result);
  },
};
