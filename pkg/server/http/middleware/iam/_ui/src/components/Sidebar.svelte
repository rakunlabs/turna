<script lang="ts">
  import SideLink from "./SideLink.svelte";
</script>

<div class="sidebar-bg border-r border-black">
  <div class="sticky top-0 overflow-auto max-h-svh no-scrollbar">
    <SideLink name="Main" path="/" />
    <SideLink name="LDAP" path="/ldap" />
  </div>
</div>

<style lang="scss">
  .sidebar-bg {
    background: url(data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAoAAAAKCAYAAACNMs+9AAAAAXNSR0IArs4c6QAAAB1JREFUKFNjvHjx4n99fX1GBgKAcVQhvhCifvAAAM43KAsXWPfwAAAAAElFTkSuQmCC)
      repeat;
  }

  /* Hide scrollbar for Chrome, Safari and Opera */
  .no-scrollbar::-webkit-scrollbar {
    display: none;
  }

  /* Hide scrollbar for IE, Edge and Firefox */
  .no-scrollbar {
    -ms-overflow-style: none; /* IE and Edge */
    scrollbar-width: none; /* Firefox */
  }
</style>
