<script>
  import { onMount } from "svelte";
  import { writable } from "svelte/store";
  import { getConfig } from "./lib/api.js";
  import Dashboard from "./lib/Dashboard.svelte";
  import Settings from "./lib/Settings.svelte";
  import MapGridding from "./lib/MapGridding.svelte";
  import { theme, activeTab } from "./lib/store.js"; // Import to initialize subscription

  const config = writable(null);
  const loading = writable(true);
  const error = writable(null);

  onMount(async () => {
    console.log("[App] MOUNTED");
    await refreshConfig();
  });

  async function refreshConfig() {
    loading.set(true);
    try {
      const c = await getConfig();
      console.log("[App] Loaded Config:", c);

      // Map and ensure defaults with correct backend field names (BigCaps)
      if (!c.Settings) c.Settings = { DefectTerms: [] };
      if (!c.Analysis) {
        c.Analysis = { TopNLimit: 20, DefaultPageSize: 20, MaxPageSize: 100 };
      } else {
        // Ensure individual fields have defaults if Analysis exists but fields are missing
        c.Analysis.TopNLimit = c.Analysis.TopNLimit || 20;
        c.Analysis.DefaultPageSize = c.Analysis.DefaultPageSize || 20;
        c.Analysis.MaxPageSize = c.Analysis.MaxPageSize || 100;
      }

      config.set(c);
    } catch (e) {
      error.set("설정 로드 실패: " + e.message);
    } finally {
      loading.set(false);
    }
  }

  function switchTab(tab) {
    activeTab.set(tab);
  }
</script>

<div class="min-h-screen bg-base-200 font-sans">
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
            type="button"
            class="rounded-xl transition-all duration-300"
            class:active={$activeTab === "dashboard"}
            class:btn-active={$activeTab === "dashboard"}
            on:click={() => switchTab("dashboard")}
          >
            📊 대시보드
          </button>
        </li>
        <li>
          <button
            type="button"
            class="rounded-xl transition-all duration-300"
            class:active={$activeTab === "settings"}
            class:btn-active={$activeTab === "settings"}
            on:click={() => switchTab("settings")}
          >
            ⚙️ 설정
          </button>
        </li>
        <li>
          <button
            type="button"
            class="rounded-xl transition-all duration-300"
            class:active={$activeTab === "mapgridding"}
            class:btn-active={$activeTab === "mapgridding"}
            on:click={() => switchTab("mapgridding")}
          >
            🧩 맵그리딩
          </button>
        </li>
      </ul>
    </div>
  </div>

  <main class="container mx-auto py-6 px-4">
    {#if $error}
      <div class="alert alert-error shadow-lg max-w-2xl mx-auto mt-10">
        <span>{$error}</span>
        <button class="btn btn-sm btn-outline" on:click={refreshConfig}
          >재시도</button
        >
      </div>
    {:else if $loading}
      <div class="flex flex-col items-center justify-center h-[50vh]">
        <span class="loading loading-bars loading-lg text-primary"></span>
        <p class="mt-4 text-gray-500">설정 로드 중...</p>
      </div>
    {:else if $config}
      {#if $activeTab === "dashboard"}
        <Dashboard config={$config} />
      {:else if $activeTab === "mapgridding"}
        <MapGridding />
      {:else}
        <Settings config={$config} on:saved={refreshConfig} />
      {/if}
    {/if}
  </main>
</div>
