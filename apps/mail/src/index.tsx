import { EmailMessage } from "cloudflare:email";
import { reportWorkerError } from "./report";
import { render } from "@react-email/render";
import type { ReactElement } from "react";
import { CodeEmail } from "./emails/code";
import { InviteEmail } from "./emails/invite";
import { LinkEmail } from "./emails/link";
import { NoticeEmail } from "./emails/notice";

interface Env {
	EMAIL: SendEmail;
	EMAIL_FROM: string;
	MAIL_TOKEN: string;
	RESEND_KEY?: string;
	TELNYX_API_KEY?: string;
	TELNYX_MESSAGING_PROFILE_ID?: string;
	TELNYX_FROM?: string;
}

interface SendRequest {
	to: string;
	channel?: string;
	subject?: string;
	html?: string;
	text?: string;
	template_type?: string;
	kind?: string;
	data?: Record<string, unknown>;
}

function mimeFromAddress(from: string): string {
	const match = from.match(/<([^>]+)>/);
	return match ? match[1] : from;
}

function str(v: unknown): string {
	return typeof v === "string" ? v : "";
}

interface Rendered {
	subject: string;
	element: ReactElement;
}

function templateFor(req: SendRequest): Rendered | null {
	const d = req.data ?? {};
	if (req.kind === "invite") {
		return {
			subject: "You're invited to Veil",
			element: <InviteEmail url={str(d.url)} code={str(d.code)} />,
		};
	}
	if (req.kind === "recovery") {
		const event = str(d.event);
		const who = str(d.human_email) || str(d.human) || "a member";
		const review = str(d.url) || "https://app.veil.nyc/requests";
		if (event === "mfa_reset") {
			return {
				subject: "Reset your Veil sign-in",
				element: (
					<LinkEmail
						action="Reset your sign-in"
						body="An org owner confirmed your account recovery and your previous two-factor credentials were cleared. Use this link to set a new password and re-enroll two-factor sign-in — it expires shortly. If you did not request this, contact your org owner now."
						button="Reset credentials"
						url={str(d.url)}
					/>
				),
			};
		}
		const started = event !== "completed";
		return {
			subject: started
				? `Veil security alert: account recovery started for ${who}`
				: `Veil: account recovery completed for ${who}`,
			element: (
				<LinkEmail
					action={started ? "Account recovery requested" : "Account recovery completed"}
					body={
						started
							? `A recovery request was filed for ${who}${str(d.reset_mfa) === "true" ? " with a two-factor reset" : ""}.${str(d.unlock_at) ? ` It unlocks at ${str(d.unlock_at)}.` : ""} If this was not you or your org, deny it now — the request dies on denial.`
							: `Recovery for ${who} was confirmed and completed: the org key rotated and the member's recovery share moved to a new key. If this was not expected, treat it as a security incident.`
					}
					button="Review in Veil"
					url={review}
				/>
			),
		};
	}
	if (req.kind === "request") {
		const agent = str(d.agent);
		const item = str(d.item);
		const action = str(d.action) || "use";
		const expires = str(d.expires);
		return {
			subject: `Veil: ${agent} asks to ${action} ${item}`,
			element: (
				<LinkEmail
					action="Approval requested"
					body={`Agent "${agent}" asked to ${action} "${item}".${expires ? ` The request expires at ${expires}.` : ""} Approving requires your owner session — this email cannot approve it.`}
					button="Review in Veil"
					url={str(d.url) || "https://app.veil.nyc/"}
				/>
			),
		};
	}
	switch (req.template_type) {
		case "recovery_code_valid":
			return {
				subject: "Your Veil recovery code",
				element: (
					<CodeEmail
						action="Recover your Veil account"
						code={str(d.recovery_code)}
					/>
				),
			};
		case "verification_code_valid":
			return {
				subject: "Verify your Veil email",
				element: (
					<CodeEmail
						action="Verify your email"
						code={str(d.verification_code)}
					/>
				),
			};
		case "login_code_valid":
			return {
				subject: "Your Veil sign-in code",
				element: (
					<CodeEmail
						action="Sign in to Veil"
						code={str(d.login_code)}
					/>
				),
			};
		case "recovery_valid":
			return {
				subject: "Reset your Veil credentials",
				element: (
					<LinkEmail
						action="Reset your credentials"
						body="A credential reset was requested for your Veil account. This link expires shortly."
						button="Reset credentials"
						url={str(d.recovery_url)}
					/>
				),
			};
		case "verification_valid":
			return {
				subject: "Verify your Veil email",
				element: (
					<LinkEmail
						action="Verify your email"
						body="Confirm this address belongs to your Veil account."
						button="Verify email"
						url={str(d.verification_url)}
					/>
				),
			};
		case "recovery_code_invalid":
		case "recovery_invalid":
			return {
				subject: "Veil account recovery attempt",
				element: (
					<NoticeEmail
						action="Recovery attempt"
						body="Someone requested a credential reset for this address, but no Veil account uses it. Veil is in private alpha — if you expected an invitation, ask your inviter to confirm the address."
					/>
				),
			};
		case "verification_code_invalid":
		case "verification_invalid":
		case "login_code_invalid":
		case "registration_code_invalid":
			return {
				subject: "Veil verification attempt",
				element: (
					<NoticeEmail
						action="Verification attempt"
						body="Someone tried to verify this address with Veil, but no account uses it. You can ignore this email."
					/>
				),
			};
		default:
			return null;
	}
}

function buildMime(from: string, to: string, subject: string, text: string, html: string): string {
	const boundary = `veil-${crypto.randomUUID()}`;
	return [
		`From: ${from}`,
		`To: ${to}`,
		`Subject: ${subject}`,
		"MIME-Version: 1.0",
		`Content-Type: multipart/alternative; boundary="${boundary}"`,
		"",
		`--${boundary}`,
		'Content-Type: text/plain; charset="utf-8"',
		"",
		text,
		`--${boundary}`,
		'Content-Type: text/html; charset="utf-8"',
		"",
		html,
		`--${boundary}--`,
	].join("\r\n");
}

export default {
	async fetch(request: Request, env: Env, ctx: ExecutionContext): Promise<Response> {
		try {
			return await this.handle(request, env);
		} catch (err) {
			reportWorkerError("mail", err, ctx, { path: new URL(request.url).pathname });
			throw err;
		}
	},

	async handle(request: Request, env: Env): Promise<Response> {
		if (request.method !== "POST") {
			return new Response("method not allowed", { status: 405 });
		}
		if (!env.MAIL_TOKEN || request.headers.get("Authorization") !== `Bearer ${env.MAIL_TOKEN}`) {
			return new Response("unauthorized", { status: 401 });
		}
		let body: SendRequest;
		try {
			body = await request.json<SendRequest>();
		} catch {
			return Response.json({ error: "invalid json" }, { status: 400 });
		}
		if (!body.to) {
			return Response.json({ error: "to required" }, { status: 400 });
		}
		if (body.channel === "sms" || /^\+\d{7,15}$/.test(body.to)) {
			const text = body.text ?? str(body.data?.body) ?? str(body.data?.message) ?? (body.html ? body.html.replace(/<[^>]+>/g, " ").replace(/\s+/g, " ").trim() : "");
			if (!text) return Response.json({ error: "sms body required" }, { status: 400 });
			if (!env.TELNYX_API_KEY || !env.TELNYX_FROM) {
				return Response.json({ error: "sms not configured" }, { status: 501 });
			}
			const smsBody: Record<string, string> = { from: env.TELNYX_FROM, to: body.to, text };
			if (env.TELNYX_MESSAGING_PROFILE_ID) smsBody.messaging_profile_id = env.TELNYX_MESSAGING_PROFILE_ID;
			const res = await fetch("https://api.telnyx.com/v2/messages", {
				method: "POST",
				headers: { Authorization: `Bearer ${env.TELNYX_API_KEY}`, "Content-Type": "application/json" },
				body: JSON.stringify(smsBody),
			});
			if (!res.ok) {
				return Response.json({ error: "sms failed", detail: await res.text() }, { status: 502 });
			}
			return Response.json({ ok: true });
		}
		const rendered = templateFor(body);
		let subject: string;
		let html: string;
		let text: string;
		if (rendered) {
			subject = rendered.subject;
			html = await render(rendered.element);
			text = await render(rendered.element, { plainText: true });
		} else if (body.html && body.subject) {
			subject = body.subject;
			html = body.html;
			text = body.text ?? body.html.replace(/<[^>]+>/g, " ").replace(/\s+/g, " ").trim();
		} else {
			return Response.json({ error: "unknown template or missing subject/html" }, { status: 400 });
		}
		try {
			if (env.RESEND_KEY) {
				const res = await fetch("https://api.resend.com/emails", {
					method: "POST",
					headers: { Authorization: `Bearer ${env.RESEND_KEY}`, "Content-Type": "application/json" },
					body: JSON.stringify({ from: env.EMAIL_FROM, to: [body.to], subject, html, text }),
				});
				if (!res.ok) {
					return Response.json({ error: "send failed", detail: await res.text() }, { status: 502 });
				}
			} else {
				const raw = buildMime(env.EMAIL_FROM, body.to, subject, text, html);
				await env.EMAIL.send(new EmailMessage(mimeFromAddress(env.EMAIL_FROM), body.to, raw));
			}
		} catch (err) {
			return Response.json({ error: "send failed", detail: String(err) }, { status: 502 });
		}
		return Response.json({ ok: true });
	},
};
