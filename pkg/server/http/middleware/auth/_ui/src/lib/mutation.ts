import { docket, messageOf, session } from "./state/session.svelte";

/** A failed refresh must not turn a completed mutation into a failed write. */
export async function runMutation(write: () => Promise<void>, refresh: () => Promise<void>, success: string, failure: string) {
  let committed = false;
  await session.run(async () => {
    await write();
    committed = true;
    docket.commit(success);
    try { await refresh(); }
    catch (err) { docket.reject(`Saved, but the list could not be refreshed: ${messageOf(err)}`); }
  }, failure);
  return committed;
}
