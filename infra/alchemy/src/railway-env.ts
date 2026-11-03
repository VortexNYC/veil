import * as Railway from "@alchemy.run/railway";
import type { Service } from "@alchemy.run/railway";

/** `${{Postgres.*}}` DSN for the `veil` database (matches `.railway/railway.ts`). */
export const pgVeilDsn = (postgres: Service) =>
  `postgresql://${Railway.ref(postgres, "PGUSER")}:${Railway.ref(postgres, "PGPASSWORD")}@${Railway.ref(postgres, "PGHOST")}:${Railway.ref(postgres, "PGPORT")}/veil`;

/** Base URL for pg_dump / WAL scripts (no database path). */
export const pgDumpBase = (postgres: Service) =>
  `postgresql://${Railway.ref(postgres, "PGUSER")}:${Railway.ref(postgres, "PGPASSWORD")}@${Railway.ref(postgres, "PGHOST")}:${Railway.ref(postgres, "PGPORT")}`;
