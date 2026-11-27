import { toast as sonnerToast } from 'svelte-sonner';

export type ToastTone = 'info' | 'error';

export function toast(message: string, tone: ToastTone = 'info'): void {
	if (tone === 'error') {
		sonnerToast.error(message);
	} else {
		sonnerToast(message);
	}
}
