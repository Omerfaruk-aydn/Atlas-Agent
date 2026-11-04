// Provider logos, from @lobehub/icons (MIT). A provider without an official
// mark gets none: nothing is drawn in its place.

import aihubmix from "@lobehub/icons-static-svg/icons/aihubmix-color.svg?raw";
import alibaba from "@lobehub/icons-static-svg/icons/alibaba-color.svg?raw";
import anthropic from "@lobehub/icons-static-svg/icons/anthropic.svg?raw";
import antigravity from "@lobehub/icons-static-svg/icons/antigravity-color.svg?raw";
import atlascloud from "@lobehub/icons-static-svg/icons/atlascloud.svg?raw";
import azure from "@lobehub/icons-static-svg/icons/azure-color.svg?raw";
import baseten from "@lobehub/icons-static-svg/icons/baseten.svg?raw";
import bedrock from "@lobehub/icons-static-svg/icons/bedrock-color.svg?raw";
import cerebras from "@lobehub/icons-static-svg/icons/cerebras-color.svg?raw";
import chutes from "@lobehub/icons-static-svg/icons/chutes.svg?raw";
import claude from "@lobehub/icons-static-svg/icons/claude-color.svg?raw";
import cohere from "@lobehub/icons-static-svg/icons/cohere-color.svg?raw";
import deepseek from "@lobehub/icons-static-svg/icons/deepseek-color.svg?raw";
import fireworks from "@lobehub/icons-static-svg/icons/fireworks-color.svg?raw";
import gemini from "@lobehub/icons-static-svg/icons/gemini-color.svg?raw";
import githubcopilot from "@lobehub/icons-static-svg/icons/githubcopilot.svg?raw";
import groq from "@lobehub/icons-static-svg/icons/groq.svg?raw";
import huggingface from "@lobehub/icons-static-svg/icons/huggingface-color.svg?raw";
import ionet from "@lobehub/icons-static-svg/icons/ionet.svg?raw";
import kimi from "@lobehub/icons-static-svg/icons/kimi-color.svg?raw";
import meta from "@lobehub/icons-static-svg/icons/meta-color.svg?raw";
import minimax from "@lobehub/icons-static-svg/icons/minimax-color.svg?raw";
import mistral from "@lobehub/icons-static-svg/icons/mistral-color.svg?raw";
import moonshot from "@lobehub/icons-static-svg/icons/moonshot.svg?raw";
import nebius from "@lobehub/icons-static-svg/icons/nebius.svg?raw";
import nvidia from "@lobehub/icons-static-svg/icons/nvidia-color.svg?raw";
import openai from "@lobehub/icons-static-svg/icons/openai.svg?raw";
import opencode from "@lobehub/icons-static-svg/icons/opencode.svg?raw";
import openrouter from "@lobehub/icons-static-svg/icons/openrouter-color.svg?raw";
import perplexity from "@lobehub/icons-static-svg/icons/perplexity-color.svg?raw";
import qiniu from "@lobehub/icons-static-svg/icons/qiniu-color.svg?raw";
import v0 from "@lobehub/icons-static-svg/icons/v0.svg?raw";
import venice from "@lobehub/icons-static-svg/icons/venice-color.svg?raw";
import vercel from "@lobehub/icons-static-svg/icons/vercel.svg?raw";
import vertexai from "@lobehub/icons-static-svg/icons/vertexai-color.svg?raw";
import xai from "@lobehub/icons-static-svg/icons/xai.svg?raw";
import xiaomi from "@lobehub/icons-static-svg/icons/xiaomimimo.svg?raw";
import yi from "@lobehub/icons-static-svg/icons/yi-color.svg?raw";
import zai from "@lobehub/icons-static-svg/icons/zai.svg?raw";
import zhipu from "@lobehub/icons-static-svg/icons/zhipu-color.svg?raw";

// First match wins, so the narrow patterns sit above the broad ones.
const RULES: [RegExp, string][] = [
  [/^vercel-v0/, v0],
  [/^bedrock/, bedrock],
  [/^azure/, azure],
  [/^(chatgpt|openai)/, openai],
  [/^claude/, claude],
  [/^anthropic/, anthropic],
  [/^antigravity/, antigravity],
  [/^gemini/, gemini],
  [/^copilot/, githubcopilot],
  [/^(muse|meta)/, meta],
  [/^kimi/, kimi],
  [/^moonshot/, moonshot],
  [/^minimax/, minimax],
  [/^(xai|grok)/, xai],
  [/^groq/, groq],
  [/^nvidia/, nvidia],
  [/^deepseek/, deepseek],
  [/^mistral/, mistral],
  [/^cohere/, cohere],
  [/^perplexity/, perplexity],
  [/^zai/, zai],
  [/^zhipu/, zhipu],
  [/^alibaba/, alibaba],
  [/^xiaomi/, xiaomi],
  [/^openrouter/, openrouter],
  [/^opencode/, opencode],
  [/^huggingface/, huggingface],
  [/^vercel/, vercel],
  [/^cerebras/, cerebras],
  [/^fireworks/, fireworks],
  [/^yi$/, yi],
  [/^aihubmix/, aihubmix],
  [/^baseten/, baseten],
  [/^chutes/, chutes],
  [/^nebius/, nebius],
  [/^venice/, venice],
  [/^ionet/, ionet],
  [/^atlascloud/, atlascloud],
  [/^vertexai/, vertexai],
  [/^qiniu/, qiniu],
];

/** The provider's mark as SVG source, or "" when it has none. */
export function logoOf(provider: string): string {
  const svg = RULES.find(([pattern]) => pattern.test(provider))?.[1] ?? "";
  return svg.replace(/<title>.*?<\/title>/, "");
}
