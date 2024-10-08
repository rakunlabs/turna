<script lang="ts">
  import { getApiPrefix, storeNavbar } from "@/store/store";
  import update from "immutability-helper";

  import { formToObject } from "@/helper/codec";

  storeNavbar.update((v) => update(v, { title: { $set: "LDAP" } }));

  let userOutput: HTMLTextAreaElement;
  let groupOutput: HTMLTextAreaElement;

  const getUser = async (
    e: SubmitEvent & { currentTarget: EventTarget & HTMLFormElement },
  ) => {
    const data = formToObject(e.currentTarget);

    if (data.uid === "") {
      console.error("Uid is required");
      return;
    }

    try {
      const res = await fetch(getApiPrefix() + "/api/ldap-users/" + data.uid, {
        method: "GET",
        headers: {
          "Content-Type": "application/json",
        },
      });

      if (!res.ok) {
        throw new Error("failed to get user info, status: " + res.status);
      }

      const user = await res.json();
      userOutput.textContent = JSON.stringify(user, null, 2);
    } catch (err) {
      console.error(err);
    }
  };

  const getGroups = async () => {
    try {
      const res = await fetch(getApiPrefix() + "/api/ldap-groups", {
        method: "GET",
        headers: {
          "Content-Type": "application/json",
        },
      });

      if (!res.ok) {
        throw new Error("failed to get groups, status: " + res.status);
      }

      const groups = await res.json();
      groupOutput.textContent = JSON.stringify(groups, null, 2);
    } catch (err) {
      console.error(err);
    }
  };
</script>

<div class="p-2">
  <div>
    <span>Ldap - get an user info</span>
    <form on:submit|preventDefault|stopPropagation={getUser}>
      <label>
        <span>Uid</span>
        <input type="text" name="uid" class="px-1" />
      </label>
      <button type="submit" class="submit-button">Submit</button>
    </form>

    <textarea bind:this={userOutput} class="code"></textarea>
  </div>

  <hr class="mt-2 mb-2" />
  <div>
    <span>Ldap - Get groups</span>
    <form on:submit|preventDefault|stopPropagation={getGroups}>
      <button type="submit" class="submit-button">Submit</button>
    </form>

    <textarea bind:this={groupOutput} class="code"></textarea>
  </div>
</div>

<style lang="scss">
  .code {
    @apply bg-white border border-black w-full block mt-2;
  }

  .submit-button {
    @apply bg-blue-500 text-white px-1 hover:bg-blue-700;
  }
</style>
