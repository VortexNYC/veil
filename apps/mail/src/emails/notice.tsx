import { Heading, Text } from "@react-email/components";
import { VeilEmail } from "./layout";

interface NoticeEmailProps {
	action: string;
	body: string;
}

export function NoticeEmail({ action, body }: NoticeEmailProps) {
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
					margin: 0,
				}}
			>
				{body}
			</Text>
		</VeilEmail>
	);
}
