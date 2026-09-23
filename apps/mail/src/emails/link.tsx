import { Button, Heading, Text } from "@react-email/components";
import { VeilEmail } from "./layout";

interface LinkEmailProps {
	action: string;
	body: string;
	button: string;
	url: string;
}

export function LinkEmail({ action, body, button, url }: LinkEmailProps) {
	return (
		<VeilEmail>
			<Heading
				style={{
					fontSize: "18px",
					fontWeight: 600,
					margin: "0 0 12px",
				}}
			>
				{action}
			</Heading>
			<Text
				style={{
					color: "#4a4a46",
					fontSize: "13px",
					lineHeight: "20px",
					margin: "0 0 20px",
				}}
			>
				{body}
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
				{button}
			</Button>
			<Text
				style={{
					color: "#8a8a85",
					fontSize: "11px",
					lineHeight: "16px",
					margin: "20px 0 0",
					wordBreak: "break-all" as const,
				}}
			>
				Or paste this link: {url}
			</Text>
		</VeilEmail>
	);
}
