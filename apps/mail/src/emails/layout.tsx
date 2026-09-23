import {
	Body,
	Container,
	Head,
	Html,
	Section,
	Text,
} from "@react-email/components";
import type { ReactNode } from "react";

const font = "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace";

export function VeilEmail({ children }: { children: ReactNode }) {
	return (
		<Html>
			<Head />
			<Body
				style={{
					backgroundColor: "#f6f6f4",
					fontFamily: font,
					padding: "32px 12px",
				}}
			>
				<Container
					style={{
						backgroundColor: "#ffffff",
						border: "1px solid #e4e4e0",
						maxWidth: "480px",
						padding: "32px",
					}}
				>
					<Text
						style={{
							fontFamily: font,
							fontSize: "13px",
							fontWeight: 700,
							letterSpacing: "0.2em",
							margin: "0 0 28px",
							textTransform: "uppercase" as const,
						}}
					>
						Veil
					</Text>
					{children}
					<Section style={{ marginTop: "32px" }}>
						<Text
							style={{
								color: "#8a8a85",
								fontSize: "11px",
								lineHeight: "16px",
								margin: 0,
							}}
						>
							Veil — the credential broker for agents. If you didn't
							request this email, you can ignore it.
						</Text>
					</Section>
				</Container>
			</Body>
		</Html>
	);
}
