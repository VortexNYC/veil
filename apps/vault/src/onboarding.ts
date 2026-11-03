import { originAPI, token } from "./auth";

export type OnboardingStep = {
  id: string;
  title: string;
  done: boolean;
  detail: string;
  human_action: string;
  agent_hint: string;
  href?: string;
};

export type OnboardingView = {
  complete: boolean;
  role: string;
  item_count: number;
  agent_count: number;
  grant_count: number;
  steps: OnboardingStep[];
  import_formats: string[];
  import_caveats?: string[];
  mcp_tools?: string[];
  mcp_url: string;
  hydra_issuer: string;
  docs?: Record<string, string>;
};

export async function onboarding(): Promise<
  { data?: OnboardingView; error?: { message: string } }
> {
  const t = token();
  if (!t) {
    return { error: { message: "signed out" } };
  }
  const res = await fetch(`${originAPI}/v1/onboarding`, {
    headers: { Authorization: `Bearer ${t}` },
  });
  if (!res.ok) {
    return { error: { message: res.statusText || "onboarding failed" } };
  }
  const data = (await res.json()) as OnboardingView;
  return { data };
}
