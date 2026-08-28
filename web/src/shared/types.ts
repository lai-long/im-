// 企微兼容消息内容与平台实体的 TS 类型。

export interface User {
  id: number;
  corp_id: number;
  userid: string;
  name: string;
}

export interface Chat {
  id: number;
  chatid: string;
  name: string;
  type: 'group' | 'direct' | 'single';
  agent_id?: number;
  bot_id?: number;
}

export interface Bot {
  id: number;
  corp_id: number;
  aibotid: string;
  name: string;
  callback_url: string;
  callback_token: string;
  callback_aes_key: string;
  callback_mode: 'encrypted' | 'plain';
  callback_verified: boolean;
  secret: string;
  keys?: ChatBotKey[];
}

export interface ChatBotKey {
  chat_id: number;
  name: string;
  webhook_key: string;
}

export interface Agent {
  id: number;
  corp_id: number;
  agentid: number;
  name: string;
  corpsecret: string;
  callback_url: string;
  callback_token: string;
  callback_aes_key: string;
  callback_mode: 'encrypted' | 'plain';
  callback_verified: boolean;
}

export interface CallbackTask {
  id: number;
  message_id: number;
  bot_id: number;
  payload: string;
  response_code: string;
  target_type: 'bot' | 'agent';
  target_id: number;
  status: 'pending' | 'processing' | 'streaming' | 'done' | 'dead';
  attempt: number;
  next_retry_at: number;
  created_at: number;
  last_error: string;
}

// 平台消息（/api/messages 与 WS 事件统一形态）。
export interface Message {
  id: number;
  msgid: string;
  chat_id: number;
  sender: string;
  sender_type: 'user' | 'bot' | 'agent';
  msgtype: string;
  content: Record<string, any>;
  mentioned?: string[];
  ts: number;
}

// WS 推送事件。
export type WsEvent =
  | { kind: 'message' | 'replay'; message: Message }
  | { kind: 'stream'; message: Message };

export interface AgentView {
  agent: Agent;
  corpid: string;
  gettoken: string;
  secret_hint: string;
}
