import { EmailMessage } from "cloudflare:email";
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
}

interface SendRequest {
	to: string;
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
	async fetch(request: Request, env: Env): Promise<Response> {
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
		const raw = buildMime(env.EMAIL_FROM, body.to, subject, text, html);
		try {
			await env.EMAIL.send(new EmailMessage(mimeFromAddress(env.EMAIL_FROM), body.to, raw));
		} catch (err) {
			return Response.json({ error: "send failed", detail: String(err) }, { status: 502 });
		}
		return Response.json({ ok: true });
	},
};
