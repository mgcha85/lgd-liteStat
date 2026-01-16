<script>
    import { createEventDispatcher, onMount } from "svelte";
    import {
        updateConfig,
        getSchedulerConfig,
        updateSchedulerConfig,
    } from "./api.js";

    export let config;

    const dispatch = createEventDispatcher();
    let saving = false;
    let error = null;
    let successMessage = null;

    // Local state initialized from prop with null safety
    let topNLimit = config?.Analysis?.TopNLimit ?? 20;
    let defaultPageSize = config?.Analysis?.DefaultPageSize ?? 20;
    let maxPageSize = config?.Analysis?.MaxPageSize ?? 100;

    // Ensure defectTerms is array
    let defectTerms = config?.Settings?.DefectTerms
        ? [...config.Settings.DefectTerms]
        : [];
    let newDefect = "";

    // Scheduler State
    let schedulerConfig = { enabled: false, interval_minutes: 60 };
    let schedulerLoading = false;

    onMount(async () => {
        try {
            schedulerLoading = true;
            const sc = await getSchedulerConfig();
            schedulerConfig = sc;
        } catch (e) {
            console.error("Failed to load scheduler config", e);
        } finally {
            schedulerLoading = false;
        }
    });

    function addDefect() {
        if (newDefect.trim()) {
            defectTerms = [...defectTerms, newDefect.trim()];
            newDefect = "";
        }
    }

    function removeDefect(index) {
        defectTerms = defectTerms.filter((_, i) => i !== index);
    }

    async function handleSave() {
        saving = true;
        error = null;
        successMessage = null;
        try {
            // 1. Update App Config
            const payload = {
                analysis: {
                    top_n_limit: parseInt(topNLimit),
                    default_page_size: parseInt(defaultPageSize),
                    max_page_size: parseInt(maxPageSize),
                },
                settings: {
                    defect_terms: defectTerms,
                },
            };
            await updateConfig(payload);

            // 2. Update Scheduler Config
            const schedPayload = {
                enabled: schedulerConfig.enabled,
                interval_minutes: parseInt(schedulerConfig.interval_minutes),
            };
            await updateSchedulerConfig(schedPayload);

            successMessage = "설정이 성공적으로 저장되었습니다.";
            dispatch("saved");

            // Auto-hide success message after 3 seconds
            setTimeout(() => {
                successMessage = null;
            }, 3000);
        } catch (e) {
            error = e.message;
        } finally {
            saving = false;
        }
    }
</script>

<div class="carbon-container p-8 max-w-5xl mx-auto mt-6 rounded-lg shadow-sm">
    <!-- Header -->
    <div class="mb-8">
        <h1 class="text-2xl font-light text-base-content mb-1">설정</h1>
        <p class="text-sm text-base-content/60">
            시스템 환경 및 분석 파라미터를 구성합니다.
        </p>
    </div>

    <!-- Notifications -->
    {#if successMessage}
        <div class="carbon-notification success">
            <svg
                xmlns="http://www.w3.org/2000/svg"
                class="h-5 w-5 text-success"
                viewBox="0 0 20 20"
                fill="currentColor"
            >
                <path
                    fill-rule="evenodd"
                    d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.707-9.293a1 1 0 00-1.414-1.414L9 10.586 7.707 9.293a1 1 0 00-1.414 1.414l2 2a1 1 0 001.414 0l4-4z"
                    clip-rule="evenodd"
                />
            </svg>
            <span>{successMessage}</span>
        </div>
    {/if}

    {#if error}
        <div class="carbon-notification error">
            <svg
                xmlns="http://www.w3.org/2000/svg"
                class="h-5 w-5 text-error"
                viewBox="0 0 20 20"
                fill="currentColor"
            >
                <path
                    fill-rule="evenodd"
                    d="M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-7 4a1 1 0 11-2 0 1 1 0 012 0zm-1-9a1 1 0 00-1 1v4a1 1 0 102 0V6a1 1 0 00-1-1z"
                    clip-rule="evenodd"
                />
            </svg>
            <span>{error}</span>
        </div>
    {/if}

    <div class="grid grid-cols-1 lg:grid-cols-2 gap-12">
        <!-- 분석 설정 -->
        <section>
            <h2 class="carbon-section-title">분석 파라미터</h2>

            <div class="carbon-form-group">
                <label class="carbon-label" for="topNLimit">상위 N개 표시</label
                >
                <input
                    id="topNLimit"
                    type="number"
                    bind:value={topNLimit}
                    class="carbon-input"
                    min="1"
                    max="100"
                />
                <p class="carbon-helper">랭킹에 표시할 장비 수 (1-100)</p>
            </div>

            <div class="carbon-form-group">
                <label class="carbon-label" for="pageSize">페이지 크기</label>
                <input
                    id="pageSize"
                    type="number"
                    bind:value={defaultPageSize}
                    class="carbon-input"
                    min="10"
                    max="100"
                />
                <p class="carbon-helper">한 페이지당 표시할 항목 수</p>
            </div>

            <div class="carbon-form-group">
                <label class="carbon-label" for="maxPageSize"
                    >최대 페이지 크기</label
                >
                <input
                    id="maxPageSize"
                    type="number"
                    bind:value={maxPageSize}
                    class="carbon-input"
                    min="10"
                    max="1000"
                />
                <p class="carbon-helper">허용되는 최대 페이지 크기</p>
            </div>
        </section>

        <!-- 스케줄러 설정 -->
        <section>
            <h2 class="carbon-section-title">스케줄러</h2>

            {#if schedulerLoading}
                <div class="flex items-center gap-2 text-base-content/60">
                    <span class="loading loading-spinner loading-sm"></span>
                    <span class="text-sm">로딩 중...</span>
                </div>
            {:else}
                <div class="carbon-form-group">
                    <div class="carbon-toggle-wrapper">
                        <input
                            id="schedulerEnabled"
                            type="checkbox"
                            class="toggle toggle-primary toggle-sm"
                            bind:checked={schedulerConfig.enabled}
                        />
                        <label
                            for="schedulerEnabled"
                            class="carbon-toggle-label">자동 수집 활성화</label
                        >
                    </div>
                    <p class="carbon-helper">
                        활성화 시 설정된 주기로 데이터를 자동 수집합니다.
                    </p>
                </div>

                <div class="carbon-form-group">
                    <label class="carbon-label" for="interval"
                        >수집 주기 (분)</label
                    >
                    <input
                        id="interval"
                        type="number"
                        bind:value={schedulerConfig.interval_minutes}
                        class="carbon-input"
                        min="1"
                        disabled={!schedulerConfig.enabled}
                    />
                    <p class="carbon-helper">데이터 수집 간격 (분 단위)</p>
                </div>
            {/if}
        </section>
    </div>

    <div class="carbon-divider"></div>

    <!-- 불량명 관리 -->
    <section>
        <h2 class="carbon-section-title">불량명 프리셋</h2>
        <p class="text-sm text-base-content/60 mb-4">
            분석 시 자주 사용하는 불량명를 미리 등록합니다.
        </p>

        <div class="flex gap-2 mb-6">
            <input
                type="text"
                bind:value={newDefect}
                placeholder="새 불량명 입력..."
                class="carbon-input flex-grow"
                on:keydown={(e) => e.key === "Enter" && addDefect()}
            />
            <button
                class="carbon-btn-primary px-6"
                on:click={addDefect}
                disabled={!newDefect.trim()}
            >
                추가
            </button>
        </div>

        <div class="min-h-32 p-4 bg-base-200/50 rounded">
            {#if defectTerms.length === 0}
                <div class="carbon-empty-state">
                    <svg
                        xmlns="http://www.w3.org/2000/svg"
                        class="h-8 w-8 mb-2 opacity-40"
                        fill="none"
                        viewBox="0 0 24 24"
                        stroke="currentColor"
                    >
                        <path
                            stroke-linecap="round"
                            stroke-linejoin="round"
                            stroke-width="1.5"
                            d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10"
                        />
                    </svg>
                    <span>등록된 불량명가 없습니다.</span>
                </div>
            {:else}
                <div class="flex flex-wrap">
                    {#each defectTerms as term, i}
                        <span class="carbon-tag">
                            {term}
                            <button
                                class="carbon-tag-remove"
                                on:click={() => removeDefect(i)}
                                aria-label="삭제"
                            >
                                ✕
                            </button>
                        </span>
                    {/each}
                </div>
            {/if}
        </div>
    </section>

    <!-- 저장 버튼 -->
    <div class="flex justify-end mt-8">
        <button
            class="carbon-btn-primary"
            on:click={handleSave}
            disabled={saving}
        >
            {#if saving}
                <span class="loading loading-spinner loading-sm mr-2"></span>
                저장 중...
            {:else}
                변경사항 저장
            {/if}
        </button>
    </div>
</div>

<style>
    /* IBM Carbon Design System Inspired Styles */
    .carbon-container {
        background: var(--fallback-b1, oklch(var(--b1) / 1));
        border: 1px solid oklch(var(--bc) / 0.1);
    }

    .carbon-section-title {
        font-size: 0.75rem;
        font-weight: 600;
        letter-spacing: 0.32px;
        text-transform: uppercase;
        color: oklch(var(--bc) / 0.6);
        margin-bottom: 1.5rem;
        padding-bottom: 0.5rem;
        border-bottom: 1px solid oklch(var(--bc) / 0.1);
    }

    .carbon-form-group {
        margin-bottom: 1.5rem;
    }

    .carbon-label {
        display: block;
        font-size: 0.75rem;
        font-weight: 400;
        color: oklch(var(--bc) / 0.7);
        margin-bottom: 0.5rem;
        letter-spacing: 0.32px;
    }

    .carbon-input {
        width: 100%;
        height: 2.5rem;
        padding: 0 1rem;
        font-size: 0.875rem;
        background: oklch(var(--b2) / 1);
        border: none;
        border-bottom: 1px solid oklch(var(--bc) / 0.3);
        outline: none;
        transition:
            border-color 0.15s,
            box-shadow 0.15s;
    }

    .carbon-input:focus {
        border-bottom-color: oklch(var(--p) / 1);
        box-shadow: inset 0 -2px 0 oklch(var(--p) / 1);
    }

    .carbon-input:hover:not(:focus) {
        border-bottom-color: oklch(var(--bc) / 0.5);
    }

    .carbon-helper {
        font-size: 0.75rem;
        color: oklch(var(--bc) / 0.5);
        margin-top: 0.25rem;
    }

    .carbon-toggle-wrapper {
        display: flex;
        align-items: center;
        gap: 1rem;
        padding: 0.75rem 0;
    }

    .carbon-toggle-label {
        font-size: 0.875rem;
        color: oklch(var(--bc) / 0.9);
    }

    .carbon-btn-primary {
        height: 3rem;
        padding: 0 4rem;
        font-size: 0.875rem;
        font-weight: 400;
        letter-spacing: 0.16px;
        background: oklch(var(--p) / 1);
        color: oklch(var(--pc) / 1);
        border: none;
        cursor: pointer;
        transition: background 0.15s;
    }

    .carbon-btn-primary:hover {
        background: oklch(var(--p) / 0.85);
    }

    .carbon-btn-primary:disabled {
        background: oklch(var(--bc) / 0.2);
        color: oklch(var(--bc) / 0.4);
        cursor: not-allowed;
    }

    .carbon-tag {
        display: inline-flex;
        align-items: center;
        gap: 0.5rem;
        height: 1.5rem;
        padding: 0 0.5rem;
        font-size: 0.75rem;
        background: oklch(var(--b2) / 1);
        border-radius: 2px;
        margin: 0.25rem;
    }

    .carbon-tag-remove {
        display: flex;
        align-items: center;
        justify-content: center;
        width: 1rem;
        height: 1rem;
        background: none;
        border: none;
        cursor: pointer;
        color: oklch(var(--bc) / 0.6);
        transition: color 0.15s;
    }

    .carbon-tag-remove:hover {
        color: oklch(var(--er) / 1);
    }

    .carbon-notification {
        display: flex;
        align-items: center;
        gap: 0.75rem;
        padding: 1rem;
        margin-bottom: 1.5rem;
        font-size: 0.875rem;
    }

    .carbon-notification.success {
        background: oklch(var(--su) / 0.1);
        border-left: 3px solid oklch(var(--su) / 1);
        color: oklch(var(--bc) / 0.9);
    }

    .carbon-notification.error {
        background: oklch(var(--er) / 0.1);
        border-left: 3px solid oklch(var(--er) / 1);
        color: oklch(var(--bc) / 0.9);
    }

    .carbon-divider {
        height: 1px;
        background: oklch(var(--bc) / 0.1);
        margin: 2rem 0;
    }

    .carbon-empty-state {
        display: flex;
        flex-direction: column;
        align-items: center;
        justify-content: center;
        padding: 2rem;
        color: oklch(var(--bc) / 0.5);
        font-size: 0.875rem;
    }
</style>
