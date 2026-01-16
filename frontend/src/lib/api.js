const API_BASE = '/api';

export async function getEquipmentRankings(filters = {}) {
    const params = new URLSearchParams();
    if (filters.start_date) params.append('start_date', filters.start_date);
    if (filters.end_date) params.append('end_date', filters.end_date);
    if (filters.defect_name) params.append('defect_name', filters.defect_name);
    if (filters.limit) params.append('limit', filters.limit);
    if (filters.facility) params.append('facility', filters.facility);

    const response = await fetch(`${API_BASE}/equipment/rankings?${params}`);
    if (!response.ok) throw new Error('Failed to fetch rankings');
    return await response.json();
}

export async function requestAnalysis(params) {
    const response = await fetch(`${API_BASE}/analyze`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(params)
    });
    if (!response.ok) throw new Error('Failed to request analysis');
    return await response.json();
}

// Helper to analyze batch
export async function analyzeBatch(req) {
    const response = await fetch(`${API_BASE}/analyze/batch`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(req),
    });

    if (!response.ok) {
        const err = await response.json();
        throw new Error(err.error || "Batch analysis failed");
    }

    // DEBUG: Read text first to see what we got
    const text = await response.text();
    console.log(`[Batch Resp] Status: ${response.status}, Size: ${text.length}`);
    if (text.length > 0 && text.length < 500) {
        console.log(`[Batch Resp Body]:`, text);
    } else if (text.length > 500) {
        console.log(`[Batch Resp Start]:`, text.substring(0, 200) + "...");
    }

    if (!text) return {};

    try {
        return JSON.parse(text);
    } catch (e) {
        console.error("JSON Parse Error:", e, "Body:", text.substring(0, 500));
        throw new Error("Failed to parse response JSON");
    }
}

export async function getAnalysisStatus(jobId) {
    const response = await fetch(`${API_BASE}/analyze/${jobId}/status`);
    if (!response.ok) throw new Error('Failed to get status');
    return await response.json();
}

export async function getAnalysisResults(jobId, limit = 100, offset = 0) {
    const params = new URLSearchParams({
        limit: String(limit),
        offset: String(offset)
    });
    const response = await fetch(`${API_BASE}/analyze/${jobId}/results?${params}`);
    if (!response.ok) throw new Error('Failed to get results');
    return await response.json();
}

export async function triggerIngest(startTime, endTime) {
    const response = await fetch(`${API_BASE}/ingest`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ start_time: startTime, end_time: endTime })
    });
    if (!response.ok) throw new Error('Failed to trigger ingestion');
    return await response.json();
}

export async function refreshMart(facility) {
    const params = new URLSearchParams();
    if (facility) params.append("facility", facility);

    const response = await fetch(`${API_BASE}/mart/refresh?${params}`, {
        method: "POST",
    });
    if (!response.ok) throw new Error("Mart refresh failed");
    return response.json();
}

export async function getConfig() {
    const response = await fetch(`${API_BASE}/config`);
    if (!response.ok) throw new Error("Failed to load config");
    return response.json();
}

export async function updateConfig(config) {
    const response = await fetch(`${API_BASE}/config`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(config),
    });
    if (!response.ok) throw new Error("Failed to update config");
    return response.json();
}

export async function streamBatchAnalysis(req, onResult) {
    const response = await fetch(`${API_BASE}/analyze/stream`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(req),
    });

    if (!response.ok) {
        throw new Error(`Stream Error: ${response.status}`);
    }

    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";

    try {
        while (true) {
            const { done, value } = await reader.read();
            if (done) break;

            buffer += decoder.decode(value, { stream: true });
            const lines = buffer.split("\n");

            // Handle all complete lines (leave the last one in buffer if incomplete)
            buffer = lines.pop();

            for (const line of lines) {
                if (line.trim()) {
                    try {
                        const json = JSON.parse(line);
                        onResult(json);
                    } catch (e) {
                        console.error("Stream Parse Error:", e, line);
                    }
                }
            }
        }

        if (buffer && buffer.trim()) {
            try {
                const json = JSON.parse(buffer);
                onResult(json);
            } catch (e) {
                console.error("Stream Final Parse Error:", e, buffer);
            }
        }

    } catch (e) {
        console.error("Stream Reader Error:", e);
        throw e;
    } finally {
        reader.releaseLock();
    }
}

export async function getAnalysisLogs(limit = 20) {
    const response = await fetch(`${API_BASE}/system/performance/requests?limit=${limit}`);
    if (!response.ok) throw new Error("Failed to fetch analysis logs");
    return response.json();
}

export async function analyzeDateRange(req) {
    const response = await fetch(`${API_BASE}/analyze/range`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(req),
    });
    if (!response.ok) throw new Error("Range analysis failed");
    return response.json();
}

export async function analyzeGlass(glassId) {
    const response = await fetch(`${API_BASE}/analyze/glass/${glassId}`);
    if (!response.ok) throw new Error("Glass analysis failed");
    return response.json();
}

export async function getHeatmapConfig() {
    const response = await fetch(`${API_BASE}/config/heatmap`);
    if (!response.ok) throw new Error("Failed to load heatmap config");
    return response.json();
}

export async function updateHeatmapConfig(config) {
    const response = await fetch(`${API_BASE}/config/heatmap`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(config),
    });
    if (!response.ok) throw new Error("Failed to update heatmap config");
    return response.json();
}

export function getExportUrl(jobId) {
    return `${API_BASE}/analyze/${jobId}/export`;
}
