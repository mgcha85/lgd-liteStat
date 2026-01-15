<script>
  import { onMount } from "svelte";
  import { getConfig } from "./lib/api.js";
  import Dashboard from "./lib/Dashboard.svelte";
  import Settings from "./lib/Settings.svelte";

  let config = null;
  let activeTab = "dashboard";
  let loading = true;
  let error = null;

  onMount(async () => {
    await refreshConfig();
  });

  async function refreshConfig() {
    loading = true;
    try {
      config = await getConfig();
      // Ensure settings struct exists if backend returned partial
      if (!config.Settings) config.Settings = { DefectTerms: [] };
    } catch (e) {
      error = "설정을 불러오는데 실패했습니다. 백엔드 상태를 확인해주세요.";
      console.error(e);
    } finally {
      loading = false;
    }
  }
</script>

<div class="min-h-screen bg-base-200 font-sans">
  <!-- Navbar -->
  <div class="navbar bg-base-100 shadow-md z-10 relative">
    <div class="flex-1">
      <a class="btn btn-ghost text-xl normal-case gap-2" href="/">
        <span class="text-primary text-2xl">🏭</span>
        <span class="font-bold text-gray-700">LGD liteStat</span>
      </a>
    </div>
    <div class="flex-none">
      <ul class="menu menu-horizontal px-1 gap-2">
        <li>
          <button
            class:active={activeTab === "dashboard"}
            class:btn-active={activeTab === "dashboard"}
            on:click={() => (activeTab = "dashboard")}
          >
            📊 대시보드
          </button>
        </li>
        <li>
          <button
            class:active={activeTab === "settings"}
            class:btn-active={activeTab === "settings"}
            on:click={() => (activeTab = "settings")}
          >
            ⚙️ 설정
          </button>
        </li>
      </ul>
    </div>
  </div>

  <!-- Main Content -->
  <main class="container mx-auto py-6 px-4">
    {#if error}
      <div class="alert alert-error shadow-lg max-w-2xl mx-auto mt-10">
        <svg
          xmlns="http://www.w3.org/2000/svg"
          class="stroke-current shrink-0 h-6 w-6"
          fill="none"
          viewBox="0 0 24 24"
          ><path
            stroke-linecap="round"
            stroke-linejoin="round"
            stroke-width="2"
            d="M10 14l2-2m0 0l2-2m-2 2l-2-2m2 2l2 2m7-2a9 9 0 11-18 0 9 9 0 0118 0z"
          /></svg
        >
        <span>{error}</span>
        <button class="btn btn-sm btn-outline" on:click={refreshConfig}
          >재시도</button
        >
      </div>
    {:else if loading && !config}
      <div class="flex flex-col items-center justify-center h-[50vh]">
        <span class="loading loading-bars loading-lg text-primary"></span>
        <p class="mt-4 text-gray-500">설정 로드 중...</p>
      </div>
    {:else if config}
      {#if activeTab === "dashboard"}
        <Dashboard {config} />
      {:else}
        <Settings {config} on:saved={refreshConfig} />
      {/if}
    {/if}
  </main>
</div>
