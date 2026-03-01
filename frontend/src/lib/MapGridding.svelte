<script>
    let defectFile = null;
    let facilityCode = "A1T";
    let partNoName = "";
    let gridN = 0;
    let gridM = 0;
    let loading = false;
    let error = null;
    let success = null;

    function handleFileChange(event) {
        defectFile = event.target.files[0];
    }

    async function processData() {
        if (!defectFile || !facilityCode || !partNoName) {
            error = "모든 필수 항목을 입력해주세요.";
            return;
        }

        loading = true;
        error = null;
        success = null;

        const formData = new FormData();
        formData.append("defect_file", defectFile);
        formData.append("facility_code", facilityCode);
        formData.append("part_no_name", partNoName);
        formData.append("N", String(gridN));
        formData.append("M", String(gridM));

        try {
            const response = await fetch("/canvas/analyze/grid", {
                method: "POST",
                body: formData,
            });

            if (!response.ok) {
                let errText = await response.text();
                throw new Error(
                    `처리 실패: ${response.status} ${response.statusText}\n${errText}`,
                );
            }

            const blob = await response.blob();
            const url = window.URL.createObjectURL(blob);
            const a = document.createElement("a");
            a.href = url;
            a.download = `processed_${facilityCode}_${partNoName}.parquet`;
            document.body.appendChild(a);
            a.click();
            a.remove();
            window.URL.revokeObjectURL(url);
            success = "처리 완료! 파일이 다운로드됩니다.";
        } catch (e) {
            error = e.message;
        } finally {
            loading = false;
        }
    }
</script>

<div class="card bg-base-100 shadow-xl max-w-2xl mx-auto mt-6">
    <div class="card-body">
        <h2 class="card-title text-2xl mb-2">🧩 맵그리딩 (Map Gridding)</h2>
        <p class="text-gray-500 mb-6">
            Defect 파일과 공장 및 모델 정보를 입력하여 새로운 Panel Address를
            부여합니다.
        </p>

        {#if error}
            <div class="alert alert-error shadow-lg mb-4">
                <span>{error}</span>
            </div>
        {/if}

        {#if success}
            <div class="alert alert-success shadow-lg mb-4">
                <span>{success}</span>
            </div>
        {/if}

        <div class="form-control w-full mb-4">
            <label class="label" for="defect-file">
                <span class="label-text font-bold">Defect Parquet File</span>
            </label>
            <input
                id="defect-file"
                type="file"
                accept=".parquet"
                class="file-input file-input-bordered w-full"
                on:change={handleFileChange}
            />
        </div>

        <div class="form-control w-full mb-4">
            <label class="label" for="facility-code">
                <span class="label-text font-bold">Facility Code</span>
            </label>
            <input
                id="facility-code"
                type="text"
                placeholder="예: A1T"
                bind:value={facilityCode}
                class="input input-bordered w-full"
            />
        </div>

        <div class="form-control w-full mb-4">
            <label class="label" for="part-no-name">
                <span class="label-text font-bold"
                    >Part No Name / Model Code</span
                >
            </label>
            <input
                id="part-no-name"
                type="text"
                placeholder="예: MODEL123"
                bind:value={partNoName}
                class="input input-bordered w-full"
            />
        </div>

        <!-- Grid Size Row -->
        <div class="flex gap-4 mb-2">
            <div class="form-control flex-1">
                <label class="label" for="grid-n">
                    <span class="label-text font-bold">N (열 분할 수)</span>
                    <span class="label-text-alt text-gray-400"
                        >0 = 원본 패널 유지</span
                    >
                </label>
                <input
                    id="grid-n"
                    type="number"
                    min="0"
                    placeholder="0"
                    bind:value={gridN}
                    class="input input-bordered w-full"
                />
            </div>
            <div class="form-control flex-1">
                <label class="label" for="grid-m">
                    <span class="label-text font-bold">M (행 분할 수)</span>
                    <span class="label-text-alt text-gray-400"
                        >0 = 원본 패널 유지</span
                    >
                </label>
                <input
                    id="grid-m"
                    type="number"
                    min="0"
                    placeholder="0"
                    bind:value={gridM}
                    class="input input-bordered w-full"
                />
            </div>
        </div>

        {#if gridN === 0 || gridM === 0}
            <div class="alert alert-info shadow-sm mb-4 py-2">
                <span class="text-sm"
                    >💡 N=0 또는 M=0 이면 원본 패널 구조를 유지하며 어드레스만
                    재명명합니다.</span
                >
            </div>
        {/if}

        <div class="card-actions justify-end mt-2">
            <button
                class="btn btn-primary"
                on:click={processData}
                disabled={loading}
            >
                {#if loading}
                    <span class="loading loading-spinner"></span>
                    처리 중...
                {:else}
                    데이터 처리 및 다운로드
                {/if}
            </button>
        </div>
    </div>
</div>
