import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ChatInput } from './ChatInput';

describe('ChatInput', () => {
  it('renders the textarea and send button', () => {
    render(<ChatInput onSend={vi.fn()} />);
    expect(screen.getByRole('textbox')).toBeInTheDocument();
    expect(screen.getByRole('button')).toBeInTheDocument();
  });

  it('calls onSend with the trimmed message on button click', async () => {
    const onSend = vi.fn();
    render(<ChatInput onSend={onSend} />);
    const textarea = screen.getByRole('textbox');
    await userEvent.type(textarea, '  hello world  ');
    await userEvent.click(screen.getByRole('button'));
    expect(onSend).toHaveBeenCalledWith('hello world');
  });

  it('calls onSend when Enter is pressed', async () => {
    const onSend = vi.fn();
    render(<ChatInput onSend={onSend} />);
    const textarea = screen.getByRole('textbox');
    await userEvent.type(textarea, 'send this{Enter}');
    expect(onSend).toHaveBeenCalledWith('send this');
  });

  it('does not call onSend when Shift+Enter is pressed', async () => {
    const onSend = vi.fn();
    render(<ChatInput onSend={onSend} />);
    const textarea = screen.getByRole('textbox');
    await userEvent.type(textarea, 'line1{Shift>}{Enter}{/Shift}');
    expect(onSend).not.toHaveBeenCalled();
  });

  it('does not call onSend when disabled', async () => {
    const onSend = vi.fn();
    render(<ChatInput onSend={onSend} disabled />);
    const textarea = screen.getByRole('textbox');
    await userEvent.type(textarea, 'msg{Enter}');
    expect(onSend).not.toHaveBeenCalled();
  });

  it('clears the textarea after sending', async () => {
    render(<ChatInput onSend={vi.fn()} />);
    const textarea = screen.getByRole<HTMLTextAreaElement>('textbox');
    await userEvent.type(textarea, 'hello{Enter}');
    expect(textarea.value).toBe('');
  });

  it('disables the send button when input is empty', () => {
    render(<ChatInput onSend={vi.fn()} />);
    expect(screen.getByRole('button')).toBeDisabled();
  });
});
