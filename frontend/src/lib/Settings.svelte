<script>
    import { createEventDispatcher } from "svelte";
    import { updateConfig } from "./api.js";

    export let config;

    const dispatch = createEventDispatcher();
    let saving = false;
    let error = null;

    // Local state initialized from prop
    let topNLimit = config.Analysis.TopNLimit;
    let defaultPageSize = config.Analysis.DefaultPageSize;
    let maxPageSize = config.Analysis.MaxPageSize;

    // Ensure defectTerms is array
    let defectTerms = config.Settings?.DefectTerms
        ? [...config.Settings.DefectTerms]
        : [];
    let newDefect = "";

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
        try {
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
            alert("Settings saved successfully!");
            dispatch("saved");
        } catch (e) {
            error = e.message;
        } finally {
            saving = false;
        }
    }
</script>

<div class="card bg-base-100 shadow-xl max-w-4xl mx-auto mt-8 rounded-3xl">
    <div class="card-body">
        <h2 class="card-title text-2xl mb-6">⚙️ Application Settings</h2>

        {#if error}
            <div class="alert alert-error mb-4 rounded-xl">
                <span>{error}</span>
            </div>
        {/if}

        <div class="grid grid-cols-1 md:grid-cols-2 gap-8">
            <!-- Analysis Settings -->
            <div>
                <h3 class="text-lg font-bold mb-4">Analysis Parameters</h3>
                <div class="form-control w-full max-w-xs">
                    <label class="label">
                        <span class="label-text">Top N Limit</span>
                    </label>
                    <input
                        type="number"
                        bind:value={topNLimit}
                        class="input input-bordered w-full max-w-xs rounded-xl"
                    />
                    <label class="label">
                        <span class="label-text-alt">Rankings to show</span>
                    </label>
                </div>

                <div class="form-control w-full max-w-xs mt-4">
                    <label class="label">
                        <span class="label-text">Page Size</span>
                    </label>
                    <input
                        type="number"
                        bind:value={defaultPageSize}
                        class="input input-bordered w-full max-w-xs rounded-xl"
                    />
                </div>
            </div>

            <!-- Defect Terms Management -->
            <div>
                <h3 class="text-lg font-bold mb-4">Defect Name Presets</h3>
                <div class="flex gap-2 mb-4">
                    <input
                        type="text"
                        bind:value={newDefect}
                        placeholder="Add new defect term"
                        class="input input-bordered flex-grow rounded-xl"
                        on:keydown={(e) => e.key === "Enter" && addDefect()}
                    />
                    <button
                        class="btn btn-primary rounded-xl"
                        on:click={addDefect}>Add</button
                    >
                </div>

                <div class="bg-base-200 rounded-2xl p-4 h-64 overflow-y-auto">
                    {#if defectTerms.length === 0}
                        <p class="text-gray-500 text-center py-4">
                            No defect terms defined.
                        </p>
                    {:else}
                        <ul class="menu bg-base-100 w-full rounded-xl">
                            {#each defectTerms as term, i}
                                <li
                                    class="flex flex-row justify-between items-center p-2 mb-1 border-b last:border-0 border-base-200 rounded-lg font-medium"
                                >
                                    <span class="bg-transparent">{term}</span>
                                    <button
                                        class="btn btn-ghost btn-xs text-error rounded-md"
                                        on:click={() => removeDefect(i)}
                                    >
                                        🗑️
                                    </button>
                                </li>
                            {/each}
                        </ul>
                    {/if}
                </div>
            </div>
        </div>

        <div class="card-actions justify-end mt-8">
            <button
                class="btn btn-primary rounded-xl"
                on:click={handleSave}
                disabled={saving}
            >
                {#if saving}
                    <span class="loading loading-spinner"></span>
                    Saving...
                {:else}
                    Save Changes
                {/if}
            </button>
        </div>
    </div>
</div>
