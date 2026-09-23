import { Button, Heading, Section, Text } from "@react-email/components";
import { VeilEmail } from "./layout";

interface InviteEmailProps {
	url: string;
	code: string;
}

export function InviteEmail({ url, code }: InviteEmailProps) {
	return (
		<VeilEmail>
			<Heading
				style={{
					fontSize: "18px",
					fontWeight: 600,
					margin: "0 0 12px",
				}}
			>
				You&apos;re invited to Veil
			</Heading>
			<Text
				style={{
					color: "#4a4a46",
					fontSize: "13px",
					lineHeight: "20px",
					margin: "0 0 20px",
				}}
			>
				Veil is the credential broker for agents — items, grants, and
				injectable secrets without ever handing your agent the plaintext.
				Veil is in private alpha; this link sets up your account.
			</Text>
			<Button
				href={url}
				style={{
					backgroundColor: "#111111",
					color: "#ffffff",
					display: "inline-block",
					fontSize: "13px",
					fontWeight: 600,
					padding: "12px 20px",
					textDecoration: "none",
				}}
			>
				Set up your account
			</Button>
			<Text
				style={{
					color: "#4a4a46",
					fontSize: "13px",
					lineHeight: "20px",
					margin: "20px 0 8px",
				}}
			>
				The page will ask for a one-time code. Yours is:
			</Text>
			<Section
				style={{
					backgroundColor: "#f6f6f4",
					border: "1px solid #e4e4e0",
					margin: "0 0 20px",
					padding: "16px",
					textAlign: "center" as const,
				}}
			>
				<Text
					style={{
						fontSize: "24px",
						fontWeight: 700,
						letterSpacing: "0.35em",
						margin: 0,
					}}
				>
					{code}
				</Text>
			</Section>
			<Text
				style={{
					color: "#8a8a85",
					fontSize: "11px",
					lineHeight: "16px",
					margin: "0",
					wordBreak: "break-all" as const,
				}}
			>
				Or paste this link: {url}
			</Text>
		</VeilEmail>
	);
}
