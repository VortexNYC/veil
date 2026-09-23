import { EmailMessage } from "cloudflare:email";

interface Env {
	EMAIL: SendEmail;
	EMAIL_FROM: string;
	MAIL_TOKEN: string;
}

interface SendRequest {
	to: string;
	subject: string;
	html: string;
}

function mimeFromAddress(from: string): string {
	const match = from.match(/<([^>]+)>/);
	return match ? match[1] : from;
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
		if (!body.to || !body.subject || !body.html) {
			return Response.json({ error: "to, subject, html required" }, { status: 400 });
		}
		const raw = [
			`From: ${env.EMAIL_FROM}`,
			`To: ${body.to}`,
			`Subject: ${body.subject}`,
			"MIME-Version: 1.0",
			'Content-Type: text/html; charset="utf-8"',
			"",
			body.html,
		].join("\r\n");
		try {
			await env.EMAIL.send(new EmailMessage(mimeFromAddress(env.EMAIL_FROM), body.to, raw));
		} catch (err) {
			return Response.json({ error: "send failed", detail: String(err) }, { status: 502 });
		}
		return Response.json({ ok: true });
	},
};
