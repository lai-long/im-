// 轻量 markdown 子集渲染（对齐企微机器人 markdown 语法），输出 React 元素，
// 不使用 dangerouslySetInnerHTML，用户输入天然安全。
// 支持：标题、加粗、斜体、行内代码、代码块、引用、链接、列表、分割线、
// <font color="info|comment|warning">、表格（降级为等宽文本）。

import React from 'react';

const FONT_COLORS: Record<string, string> = {
  info: '#2e9e5b',
  comment: '#888888',
  warning: '#e67e22',
};

// 行内语法：code / font / bold / italic / link
const INLINE_RE =
  /(`[^`\n]+`)|(<font color="(?:info|comment|warning)">[\s\S]*?<\/font>)|(\*\*[^*\n]+\*\*)|(\*[^*\n]+\*)|(\[[^\]\n]+\]\([^)\n]+\))/g;

function renderInline(text: string, keyPrefix: string): React.ReactNode[] {
  const out: React.ReactNode[] = [];
  let last = 0;
  let i = 0;
  let m: RegExpExecArray | null;
  INLINE_RE.lastIndex = 0;
  while ((m = INLINE_RE.exec(text)) !== null) {
    if (m.index > last) out.push(text.slice(last, m.index));
    const tok = m[0];
    const key = `${keyPrefix}-${i++}`;
    if (m[1]) {
      out.push(<code key={key} className="md-code">{tok.slice(1, -1)}</code>);
    } else if (m[2]) {
      const color = /color="(\w+)"/.exec(tok)?.[1] ?? '';
      const inner = tok.replace(/^<font color="\w+">/, '').replace(/<\/font>$/, '');
      out.push(
        <span key={key} style={{ color: FONT_COLORS[color] || undefined }}>
          {renderInline(inner, key)}
        </span>,
      );
    } else if (m[3]) {
      out.push(<strong key={key}>{renderInline(tok.slice(2, -2), key)}</strong>);
    } else if (m[4]) {
      out.push(<em key={key}>{renderInline(tok.slice(1, -1), key)}</em>);
    } else if (m[5]) {
      const lm = /^\[([^\]]+)\]\(([^)]+)\)$/.exec(tok);
      out.push(
        <a key={key} href={lm?.[2]} target="_blank" rel="noreferrer">
          {lm?.[1]}
        </a>,
      );
    }
    last = m.index + tok.length;
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}

type Block =
  | { kind: 'heading'; level: number; text: string }
  | { kind: 'quote'; lines: string[] }
  | { kind: 'code'; lines: string[] }
  | { kind: 'ul'; items: string[] }
  | { kind: 'ol'; items: string[] }
  | { kind: 'hr' }
  | { kind: 'table'; lines: string[] }
  | { kind: 'para'; lines: string[] };

function parseBlocks(src: string): Block[] {
  const lines = src.replace(/\r\n/g, '\n').split('\n');
  const blocks: Block[] = [];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    if (!line.trim()) { i++; continue; }

    const fence = /^```/.exec(line);
    if (fence) {
      const buf: string[] = [];
      i++;
      while (i < lines.length && !/^```/.test(lines[i])) buf.push(lines[i++]);
      i++; // 跳过结束 fence
      blocks.push({ kind: 'code', lines: buf });
      continue;
    }

    const head = /^(#{1,6})\s+(.*)$/.exec(line);
    if (head) {
      blocks.push({ kind: 'heading', level: head[1].length, text: head[2] });
      i++;
      continue;
    }

    if (/^\s*(---+|\*\*\*+)\s*$/.test(line)) {
      blocks.push({ kind: 'hr' });
      i++;
      continue;
    }

    if (/^\s*>/.test(line)) {
      const buf: string[] = [];
      while (i < lines.length && /^\s*>/.test(lines[i])) {
        buf.push(lines[i].replace(/^\s*>\s?/, ''));
        i++;
      }
      blocks.push({ kind: 'quote', lines: buf });
      continue;
    }

    if (/^\s*[-*]\s+/.test(line)) {
      const buf: string[] = [];
      while (i < lines.length && /^\s*[-*]\s+/.test(lines[i])) {
        buf.push(lines[i].replace(/^\s*[-*]\s+/, ''));
        i++;
      }
      blocks.push({ kind: 'ul', items: buf });
      continue;
    }

    if (/^\s*\d+\.\s+/.test(line)) {
      const buf: string[] = [];
      while (i < lines.length && /^\s*\d+\.\s+/.test(lines[i])) {
        buf.push(lines[i].replace(/^\s*\d+\.\s+/, ''));
        i++;
      }
      blocks.push({ kind: 'ol', items: buf });
      continue;
    }

    if (/^\s*\|.*\|\s*$/.test(line)) {
      const buf: string[] = [];
      while (i < lines.length && /^\s*\|.*\|\s*$/.test(lines[i])) buf.push(lines[i++]);
      blocks.push({ kind: 'table', lines: buf });
      continue;
    }

    const buf: string[] = [];
    while (
      i < lines.length &&
      lines[i].trim() &&
      !/^(#{1,6}\s|```|\s*>|\s*[-*]\s|\s*\d+\.\s|\s*\|.*\|\s*$|\s*(---+|\*\*\*+)\s*$)/.test(lines[i])
    ) {
      buf.push(lines[i++]);
    }
    blocks.push({ kind: 'para', lines: buf });
  }
  return blocks;
}

export function Markdown({ text }: { text: string }) {
  const blocks = parseBlocks(text);
  return (
    <div className="md">
      {blocks.map((b, i) => {
        switch (b.kind) {
          case 'heading': {
            const Tag = `h${Math.min(b.level + 2, 6)}` as keyof JSX.IntrinsicElements;
            return <Tag key={i} className="md-h">{renderInline(b.text, `h${i}`)}</Tag>;
          }
          case 'quote':
            return (
              <blockquote key={i} className="md-quote">
                {b.lines.map((l, j) => <div key={j}>{renderInline(l, `q${i}-${j}`)}</div>)}
              </blockquote>
            );
          case 'code':
            return <pre key={i} className="md-pre"><code>{b.lines.join('\n')}</code></pre>;
          case 'ul':
            return (
              <ul key={i} className="md-list">
                {b.items.map((it, j) => <li key={j}>{renderInline(it, `u${i}-${j}`)}</li>)}
              </ul>
            );
          case 'ol':
            return (
              <ol key={i} className="md-list">
                {b.items.map((it, j) => <li key={j}>{renderInline(it, `o${i}-${j}`)}</li>)}
              </ol>
            );
          case 'hr':
            return <hr key={i} className="md-hr" />;
          case 'table':
            // 表格降级为等宽文本，保证可读且零解析风险
            return <pre key={i} className="md-pre">{b.lines.join('\n')}</pre>;
          default:
            return (
              <p key={i} className="md-p">
                {b.lines.map((l, j) => (
                  <React.Fragment key={j}>
                    {j > 0 && <br />}
                    {renderInline(l, `p${i}-${j}`)}
                  </React.Fragment>
                ))}
              </p>
            );
        }
      })}
    </div>
  );
}
