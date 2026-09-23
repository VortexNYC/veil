import { Heading, Section, Text } from "@react-email/components";
import { VeilEmail } from "./layout";

interface CodeEmailProps {
	action: string;
	code: string;
}

export function CodeEmail({ action, code }: CodeEmailProps) {
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
				Enter this code to continue. It expires shortly — request a new
				one if it doesn't work.
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
		</VeilEmail>
	);
}
