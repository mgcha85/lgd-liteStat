/** @type {import('tailwindcss').Config} */
export default {
    content: ['./src/**/*.{html,js,svelte,ts}'],
    theme: {
        extend: {},
    },
    plugins: [require('daisyui')],
    daisyui: {
        themes: [
            "corporate",
            {
                "lgd-dark": {
                    "primary": "#3b82f6",
                    "secondary": "#64748b",
                    "accent": "#06b6d4",
                    "neutral": "#1e293b",
                    "base-100": "#0f172a",
                    "base-200": "#1e293b",
                    "base-300": "#334155",
                    "base-content": "#f8fafc",
                    "info": "#3abff8",
                    "success": "#36d399",
                    "warning": "#fbbd23",
                    "error": "#f87272",
                },
            },
        ],
    },
}
