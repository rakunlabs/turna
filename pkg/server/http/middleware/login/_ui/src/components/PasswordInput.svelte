<script lang="ts">
  export let id: string;
  export let name = "password";
  export let value = "";
  export let required = false;
  export let minlength: number | null = null;
  export let autocomplete = "current-password";
  export let placeholder = "";

  let show = false;

  const inputClass =
    "py-1.5 pl-3 pr-10 border rounded-md border-gray-300 focus:border-blue-300 focus:outline-none focus:ring focus:ring-blue-200 focus:ring-opacity-50 disabled:bg-gray-100 mt-1 block w-full";
</script>

<div class="relative">
  <input
    {id}
    {name}
    {required}
    {placeholder}
    {autocomplete}
    minlength={minlength ?? undefined}
    type={show ? "text" : "password"}
    {value}
    class={inputClass}
  />
  <button
    type="button"
    aria-label={show ? "Hide password" : "Show password"}
    on:click={() => (show = !show)}
    class="absolute inset-y-0 end-0 flex items-center z-20 px-3 cursor-pointer text-gray-400 rounded-e-md focus:outline-none focus:text-blue-600"
  >
    <svg
      class="shrink-0 size-3.5"
      width="24"
      height="24"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="2"
      stroke-linecap="round"
      stroke-linejoin="round"
    >
      <path class={show ? "hidden" : ""} d="M9.88 9.88a3 3 0 1 0 4.24 4.24"></path>
      <path class={show ? "hidden" : ""} d="M10.73 5.08A10.43 10.43 0 0 1 12 5c7 0 10 7 10 7a13.16 13.16 0 0 1-1.67 2.68"></path>
      <path class={show ? "hidden" : ""} d="M6.61 6.61A13.526 13.526 0 0 0 2 12s3 7 10 7a9.74 9.74 0 0 0 5.39-1.61"></path>
      <line class={show ? "hidden" : ""} x1="2" x2="22" y1="2" y2="22"></line>
      <path class={show ? "block" : "hidden"} d="M2 12s3-7 10-7 10 7 10 7-3 7-10 7-10-7-10-7Z"></path>
      <circle class={show ? "block" : "hidden"} cx="12" cy="12" r="3"></circle>
    </svg>
  </button>
</div>
