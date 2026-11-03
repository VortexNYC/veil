/** Process env prefix for plan/deploy digests (`load-*-env.mjs`). */
export const railwayPlainEnvKey = (railwayService: string, varName: string) =>
  `VEIL_RAILWAY_${railwayService}_${varName}`;
