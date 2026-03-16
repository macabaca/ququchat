export type LLMMessageRole = 'system' | 'user' | 'assistant';

export interface LLMMessage {
    role: LLMMessageRole;
    content: string;
}

export interface LLMConfig {
    baseUrl: string;
    apiKey: string;
    model: string;
    temperature?: number;
}

export interface LLMStreamOptions {
    config: LLMConfig;
    messages: LLMMessage[];
    onDelta: (delta: string) => void;
    signal?: AbortSignal;
}

const buildChatCompletionsUrl = (baseUrl: string) => {
    const trimmed = baseUrl.replace(/\/+$/, '');
    if (trimmed.endsWith('/chat/completions')) return trimmed;
    if (trimmed.endsWith('/v1')) return `${trimmed}/chat/completions`;
    return `${trimmed}/v1/chat/completions`;
};

const readStreamLines = async (
    reader: ReadableStreamDefaultReader<Uint8Array>,
    onLine: (line: string) => void
) => {
    const decoder = new TextDecoder();
    let buffer = '';
    while (true) {
        const { value, done } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split('\n');
        buffer = lines.pop() || '';
        for (const line of lines) {
            const trimmed = line.trim();
            if (trimmed.length > 0) onLine(trimmed);
        }
    }
    if (buffer.trim().length > 0) onLine(buffer.trim());
};

export const llmService = {
    async sendMessageStream(options: LLMStreamOptions): Promise<string> {
        const { config, messages, onDelta, signal } = options;
        const url = buildChatCompletionsUrl(config.baseUrl);
        const res = await fetch(url, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                Authorization: `Bearer ${config.apiKey}`
            },
            body: JSON.stringify({
                model: config.model,
                messages,
                temperature: config.temperature ?? 0.7,
                stream: true
            }),
            signal
        });

        if (!res.ok) {
            const text = await res.text();
            throw new Error(text || `LLM request failed: ${res.status}`);
        }

        if (!res.body) {
            const json = await res.json();
            const content = json?.choices?.[0]?.message?.content ?? '';
            if (content) onDelta(content);
            return content;
        }

        const reader = res.body.getReader();
        let fullText = '';

        await readStreamLines(reader, (line) => {
            if (!line.startsWith('data:')) return;
            const data = line.slice(5).trim();
            if (data === '[DONE]') return;
            try {
                const json = JSON.parse(data);
                const delta =
                    json?.choices?.[0]?.delta?.content ??
                    json?.choices?.[0]?.message?.content ??
                    '';
                if (delta) {
                    fullText += delta;
                    onDelta(delta);
                }
            } catch {
            }
        });

        return fullText;
    }
};
