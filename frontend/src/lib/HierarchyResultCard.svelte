<script>
    import { onMount, onDestroy } from "svelte";
    import * as echarts from "echarts";
    import { chartMode } from "./store.js";

    export let result;
    export let index;

    let chartDiv;
    let chartInstance = null;

    $: label = buildLabel(result);
    $: if (result && $chartMode === "interactive" && chartDiv) {
        setTimeout(renderInteractiveChart, 0);
    }

    function buildLabel(r) {
        let parts = [r.process_code];
        if (r.equipment_line_id) parts.push(r.equipment_line_id);
        if (r.equipment_machine_id) parts.push(r.equipment_machine_id);
        if (r.equipment_path_id) parts.push(r.equipment_path_id);
        return parts.join(" > ");
    }

    function renderInteractiveChart() {
        if (!result?.daily_dpu || result.daily_dpu.length === 0) return;
        if (!chartDiv) return;

        if (chartInstance) chartInstance.dispose();
        chartInstance = echarts.init(chartDiv);

        const sorted = [...result.daily_dpu].sort(
            (a, b) => a.work_date.localeCompare(b.work_date)
        );

        chartInstance.setOption({
            title: {
                text: `DPU Trend`,
                left: "center",
                textStyle: { fontSize: 12 },
            },
            grid: { top: 30, bottom: 30, left: 50, right: 20 },
            tooltip: {
                trigger: "axis",
                formatter: (params) => {
                    const p = params[0];
                    return `${p.axisValue}<br/>DPU: ${p.value[1].toFixed(4)}`;
                },
            },
            xAxis: {
                type: "time",
                splitLine: { show: false },
                axisLabel: {
                    rotate: 45,
                    formatter: {
                        year: "{yyyy}",
                        month: "{MM}-{dd}",
                        day: "{MM}-{dd}",
                    },
                },
            },
            yAxis: { type: "value", name: "DPU" },
            series: [
                {
                    name: "DPU",
                    type: "line",
                    data: sorted.map((d) => [d.work_date, d.dpu]),
                    showSymbol: true,
                    symbolSize: 4,
                    lineStyle: { color: "#3498db", width: 2 },
                    itemStyle: { color: "#2980b9" },
                },
            ],
        });
    }

    onDestroy(() => {
        if (chartInstance) {
            chartInstance.dispose();
            chartInstance = null;
        }
    });
</script>

<div class="card bg-base-100 shadow-md mb-4 border border-base-200">
    <div class="card-body p-4">
        <div class="flex items-center justify-between mb-2">
            <h3 class="font-bold text-sm flex items-center gap-2">
                <span class="badge badge-sm badge-primary">{index + 1}</span>
                {label}
            </h3>
            <div class="flex gap-3 text-xs text-base-content/60">
                <span>Products: <b>{result.total_products?.toLocaleString()}</b></span>
                <span>Defects: <b>{result.total_defects?.toLocaleString()}</b></span>
                <span>DPU: <b class="text-error">{result.dpu?.toFixed(4)}</b></span>
            </div>
        </div>

        {#if result.daily_dpu && result.daily_dpu.length > 0}
            {#if $chartMode === "image" && result.chart_url}
                <div class="flex justify-center">
                    <img
                        src={result.chart_url}
                        alt="DPU Trend - {label}"
                        class="max-w-full h-auto rounded border border-base-200"
                        style="max-height: 300px;"
                    />
                </div>
            {:else}
                <div
                    bind:this={chartDiv}
                    class="w-full border rounded"
                    style="height: 250px;"
                ></div>
            {/if}
        {:else}
            <div class="flex items-center justify-center h-32 text-base-content/40 text-sm">
                DPU Trend data unavailable
            </div>
        {/if}
    </div>
</div>
