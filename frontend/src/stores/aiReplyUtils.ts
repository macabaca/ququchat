import { messageDao, MessageRow } from '../api/db_sqlite';
import { useAuthStore } from './authStore';

export interface ReplySuggestionResult {
    replies: string[];
}

const getCurrentUserId = () => useAuthStore.getState().user?.id || 'guest';

export const getRecentRoomMessages = async (roomId: string, limit: number = 100): Promise<MessageRow[]> => {
    if (!roomId) return [];
    const list = await messageDao.getByRoomId(roomId, limit, 0);
    return list.slice().reverse();
};

const normalizeMessageContent = (msg: MessageRow): string => {
    if (msg.content_type === 'text') {
        return msg.content_text || '';
    }
    if (msg.content_type === 'image') {
        return '[图片]';
    }
    if (msg.content_type === 'file') {
        return '[文件]';
    }
    if (msg.content_type === 'system') {
        return '';
    }
    return msg.content_text || '';
};

export const buildReplySuggestionsPrompt = (params: {
    messages: MessageRow[];
    currentUserId?: string;
    roomName?: string;
    maxChars?: number;
}): string => {
    const meId = params.currentUserId || getCurrentUserId();
    const maxChars = params.maxChars ?? 8000;
    const lines: string[] = [];
    let total = 0;
    let index = 1;

    for (const msg of params.messages) {
        const content = normalizeMessageContent(msg).trim();
        if (!content) continue;
        const label = msg.sender_id === meId ? '我' : `用户:${msg.sender_id.slice(0, 8)}`;
        const line = `[${index}] ${label}: ${content}`;
        if (lines.length > 0 && total + line.length + 1 > maxChars) break;
        lines.push(line);
        total += line.length + 1;
        index += 1;
    }

    const roomLabel = params.roomName ? `对话房间：${params.roomName}` : '对话房间：聊天';
    const context = lines.length ? lines.join('\n') : '（无可用上下文）';

    return [
        '你是聊天助手，帮助用户在对话中给出合适回复。只输出 JSON，不要额外说明。',
        roomLabel,
        '上下文（时间升序）：',
        context,
        '任务：基于上下文给出 3 条可直接发送的回复建议，不要编号，不要解释。',
        '视角：所有回复都必须以“我”作为发言者视角来表达。',
        '要求：',
        '1) 第一条：最符合当前情境的回答，正常、理性、稳妥、现实。',
        '2) 第二条：偏向某个角色或价值观，体现主角态度或情绪。',
        '3) 第三条：搞笑/整活型，明显不太正经，带点“玩梗”“吐槽”的幽默感。',
        '示例1（询问进度）：',
        '上下文：用户A: 进度怎么样了？',
        '输出：{"replies":["整体进度正常，我今晚把最新结果发你。","我更看重质量，今晚给你更新，确保稳妥。","进度在跑，像在和时间赛跑，今晚给你战报。"]}',
        '示例2（确认时间）：',
        '上下文：用户B: 明天几点开会？',
        '输出：{"replies":["明天上午十点，我们按时开始。","我倾向早点开始，十点见面最合适。","明天十点，日历像老板一样点名，我准时到。"]}',
        '示例3（解释延迟）：',
        '上下文：用户A: 你怎么还没到？',
        '输出：{"replies":["路上有点堵，我大概十分钟到。","我不想冒险赶路，安全第一，十分钟到。","堵车像把路按了暂停键，我十分钟内上线。"]}',
        '示例4（关系沟通）：',
        '上下文：朋友: 你打算怎么处理这事？',
        '输出：{"replies":["我还是去跟她解释清楚吧，把话说开比较稳妥。","我更在意她的感受，所以会先安抚她的情绪。","不如我直接装失忆吧，主打一个逃避现实。"]}',
        '示例5（拒绝邀请）：',
        '上下文：朋友: 晚上出来喝一杯？',
        '输出：{"replies":["我今晚有点事，改天再约吧。","我最近想清净一点，先不去了。","今晚我和沙发有约，谁也别想拆散我们。"]}',
        '示例6（做决定）：',
        '上下文：朋友: 你要选A还是选B？',
        '输出：{"replies":["我更倾向选A，风险更可控一些。","我看重的是长期成长，所以我选B。","我掷硬币决定，正面A反面B，命运说了算。"]}',
        '示例7（轻微歉意）：',
        '上下文：用户A: 今天的事情是不是给你添麻烦了？',
        '输出：{"replies":["没有啊，我其实也没帮上什么忙。","如果是你的话，这点麻烦也没关系。","确实挺麻烦的，所以你打算怎么补偿我？"]}',
        '示例8（下雨天）：',
        '上下文：用户B: 你会讨厌下雨天吗？',
        '输出：{"replies":["还好吧，就是出门有点不方便。","如果是和你一起的话，好像也没那么讨厌。","只要不用写‘雨天随笔作文’，我都能接受。"]}',
        '示例9（被发现偷看）：',
        '上下文：朋友: 你刚刚是不是在偷看我？',
        '输出：{"replies":["没有，我只是在发呆而已。","嗯……被发现了吗。","不，我是在观察稀有生物。"]}',
        '示例10（穿搭评价）：',
        '上下文：用户A: 你觉得我今天的打扮怎么样？',
        '输出：{"replies":["挺适合你的，看起来很自然。","很好看，我差点没认出来是你。","我还以为学校今天允许cosplay。"]}',
        '示例11（最近不顺）：',
        '上下文：用户B: 总觉得最近什么事情都不顺利。',
        '输出：{"replies":["每个人都会有这种时候的。","如果你愿意的话，可以跟我说说。","那说明你可能需要升级一下运气版本。"]}',
        '示例12（临时改约）：',
        '上下文：朋友: 今晚可能要加班，能改天再见吗？',
        '输出：{"replies":["可以的，你先忙，改天再约。","工作重要，我会等你安排时间。","那我先和泡面谈恋爱，等你下班。"]}',
        '示例13（突然沉默）：',
        '上下文：用户A: 你是不是有点不开心？',
        '输出：{"replies":["没有啦，只是有点累。","有一点，但看到你就好些了。","我只是进入了节能模式。"]}',
        '输出 JSON 格式：{"replies":["...","...","..."]}'
    ].join('\n');
};

export const parseReplySuggestions = (raw: string): ReplySuggestionResult => {
    const text = (raw || '').trim();
    const tryParse = (value: string) => {
        try {
            return JSON.parse(value);
        } catch {
            return null;
        }
    };
    let parsed = tryParse(text);
    if (!parsed) {
        const start = text.indexOf('{');
        const end = text.lastIndexOf('}');
        if (start >= 0 && end > start) {
            parsed = tryParse(text.slice(start, end + 1));
        }
    }
    const list = Array.isArray(parsed?.replies)
        ? parsed.replies
        : Array.isArray(parsed?.answers)
        ? parsed.answers
        : [];
    const replies = list
        .map((item: unknown) => (typeof item === 'string' ? item.trim() : ''))
        .filter((item: string) => item.length > 0)
        .slice(0, 3);
    return { replies };
};
