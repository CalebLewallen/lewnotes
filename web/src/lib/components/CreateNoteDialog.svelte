<script lang="ts">
	import { untrack } from 'svelte';

	import { api, ApiError, type Note } from '#lib/api';
	import { frontmatterForTitle } from '#lib/frontmatter';
	import { titleFromPath } from '#lib/paths';
	import { Button, buttonVariants } from '$lib/components/ui/button/index.js';
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import * as Field from '$lib/components/ui/field/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';

	interface Props {
		open?: boolean;
		initialPath?: string;
		initialTitle?: string;
		oncreated?: (note: Note) => void;
	}

	let {
		open = $bindable(false),
		initialPath = 'Untitled.md',
		initialTitle = '',
		oncreated
	}: Props = $props();

	const formID = `create-note-${Math.random().toString(36).slice(2)}`;
	const pathID = `${formID}-path`;
	const contentID = `${formID}-content`;
	let path = $state(untrack(() => initialPath));
	let content = $state('');
	let error = $state('');
	let busy = $state(false);
	let wasOpen = false;

	$effect(() => {
		if (open && !wasOpen) {
			path = initialPath || 'Untitled.md';
			content = '';
			error = '';
			busy = false;
		}
		wasOpen = open;
	});

	async function create(event: SubmitEvent) {
		event.preventDefault();
		if (busy) return;
		busy = true;
		error = '';
		try {
			const title = initialTitle || titleFromPath(path);
			const base = frontmatterForTitle(title);
			const noteContent = content.trim() ? `${base}${content.trimEnd()}\n` : base;
			const note = await api.createNote(fetch, { path, content: noteContent });
			open = false;
			oncreated?.(note);
		} catch (e) {
			error = e instanceof ApiError ? e.message : 'Could not create the note.';
		} finally {
			busy = false;
		}
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="sm:max-w-lg">
		<form id={formID} class="space-y-5" onsubmit={create}>
			<Dialog.Header>
				<Dialog.Title>New note</Dialog.Title>
				<Dialog.Description>The server adds .md when you leave it off.</Dialog.Description>
			</Dialog.Header>

			<Field.Group>
				<Field.Field>
					<Field.Label for={pathID}>Vault path</Field.Label>
					<Input
						id={pathID}
						class="font-mono text-[13px]"
						bind:value={path}
						required
						spellcheck="false"
						autocomplete="off"
						placeholder="Untitled.md"
					/>
					<Field.Description>Relative to the vault root.</Field.Description>
				</Field.Field>

				<Field.Field>
					<Field.Label for={contentID}>Starting text</Field.Label>
					<Textarea
						id={contentID}
						class="min-h-32 font-mono text-[13px]"
						bind:value={content}
						spellcheck="true"
					/>
				</Field.Field>
			</Field.Group>

			{#if error}
				<p class="text-destructive text-sm" role="alert">{error}</p>
			{/if}

			<Dialog.Footer>
				<Dialog.Close class={buttonVariants({ variant: 'outline' })}>Cancel</Dialog.Close>
				<Button type="submit" disabled={busy}>
					{busy ? 'Creating…' : 'Create note'}
				</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
