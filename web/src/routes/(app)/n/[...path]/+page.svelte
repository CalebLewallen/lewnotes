<script lang="ts">
	import { beforeNavigate, goto } from '$app/navigation';
	import { page } from '$app/state';
	import XIcon from '@lucide/svelte/icons/x';
	import { onMount, tick, untrack } from 'svelte';

	import { api, ApiError, fileHref, formatDate, noteHref, type Note } from '#lib/api';
	import ConflictDiff from '#lib/components/ConflictDiff.svelte';
	import CreateNoteDialog from '#lib/components/CreateNoteDialog.svelte';
	import MarkdownEditor from '#lib/components/MarkdownEditor.svelte';
	import NoteProperties from '#lib/components/NoteProperties.svelte';
	import type { WikilinkClick } from '#lib/editor/live-markdown';
	import {
		customProperties,
		ensureFrontmatterID,
		frontmatterId,
		frontmatterTags,
		frontmatterTitle,
		joinFrontmatter,
		replaceCustomProperties,
		setTags,
		setTitle,
		splitFrontmatter
	} from '#lib/frontmatter';
	import { live } from '#lib/live.svelte.js';
	import { ghostNotePath, isImageTarget, notePathForTitle, titleFromPath } from '#lib/paths';
	import { toast } from '#lib/toast';
	import { notePaths, useVaultTree } from '#lib/vault-tree';
	import * as Alert from '$lib/components/ui/alert/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();

	const initialNote = untrack(() => data.note);
	const initialContent = initialNote.content ?? initialNote.body;
	const initialSplit = splitFrontmatter(initialContent);
	const initialTitle = frontmatterTitle(initialSplit.frontmatter) || initialNote.title || titleFromPath(initialNote.path);
	const initialSaved = joinFrontmatter(initialSplit.frontmatter, initialSplit.body);

	let note = $state<Note>(initialNote);
	let frontmatter = $state(initialSplit.frontmatter);
	let body = $state(initialSplit.body);
	let savedContent = $state(initialSaved);
	let etag = $state(initialNote.etag);
	let title = $state(initialTitle);
	let saving = $state(false);
	let error = $state('');
	let conflict = $state<{ server: Note; local: string } | null>(null);
	let remote = $state<Note | null>(null);
	let deleted = $state(false);
	let createOpen = $state(false);
	let propertiesOpen = $state(false);
	let ghostTarget = $state('');
	let tagInputOpen = $state(false);
	let tagDraft = $state('');
	let titleEl = $state<HTMLTextAreaElement | null>(null);
	let titleAtFocus = '';
	let titleRenameBusy = false;
	let seenRevision = 0;
	let savePromise: Promise<boolean> | null = null;
	let saveAgain = false;

	const currentContent = $derived(joinFrontmatter(frontmatter, body));
	const dirty = $derived(currentContent !== savedContent);
	const tags = $derived(frontmatterTags(frontmatter));
	const properties = $derived(customProperties(frontmatter));
	const displayTitle = $derived(title.trim() || note.title || titleFromPath(note.path));
	const issues = $derived(
		note.links.filter((link) => link.resolution === 'unresolved' || link.resolution === 'ambiguous')
	);
	const ghostPath = $derived(ghostTarget ? ghostNotePath(ghostTarget, note.path) : '');
	const ghostTitle = $derived(ghostTarget ? titleFromPath(ghostTarget) : '');
	const getVaultTree = useVaultTree();

	$effect(() => {
		const next = data.note;
		if (next.id === note.id) return;
		applyNote(next);
		conflict = null;
		remote = null;
		deleted = false;
		error = '';
		propertiesOpen = false;
		tagInputOpen = false;
	});

	$effect(() => {
		const target = currentContent;
		const saved = savedContent;
		const blocked = conflict !== null || deleted || saving || error !== '';
		if (blocked || target === saved) return;
		const timer = setTimeout(() => void save(), 900);
		return () => clearTimeout(timer);
	});

	$effect(() => {
		const revision = live.revision;
		if (!revision || revision === seenRevision) return;
		seenRevision = revision;
		const event = live.last;
		if (!event || deleted) return;
		// Ignore our own save echo while its response is still in flight.
		if (event.origin === 'web' && saving) return;
		if (event.type === 'resync') {
			void refreshFromServer();
			return;
		}
		if (event.type === 'tree.changed') {
			void refreshFromServer();
			return;
		}
		if (event.type === 'note.deleted' && (event.id === note.id || event.path === note.path)) {
			deleted = true;
			conflict = null;
			remote = null;
			toast('This note was deleted elsewhere.', 'error');
			return;
		}
		// The event for a change this tab already applied is not remote work.
		if (event.type === 'note.updated' && event.etag && event.etag === etag) return;
		if (event.type === 'note.moved' && event.path === note.path && event.etag === etag) return;
		if (event.id === note.id || event.path === note.path) void refreshFromServer();
	});

	onMount(() => {
		const beforeUnload = (event: BeforeUnloadEvent) => {
			if (!dirty) return;
			event.preventDefault();
			event.returnValue = '';
		};
		window.addEventListener('beforeunload', beforeUnload);
		void tick().then(resizeTitle);
		return () => window.removeEventListener('beforeunload', beforeUnload);
	});

	function applyNote(next: Note) {
		const raw = next.content ?? next.body;
		const split = splitFrontmatter(raw);
		note = next;
		etag = next.etag;
		frontmatter = split.frontmatter;
		body = split.body;
		savedContent = joinFrontmatter(split.frontmatter, split.body);
		title = frontmatterTitle(split.frontmatter) || next.title || titleFromPath(next.path);
		conflict = null;
		remote = null;
		void tick().then(resizeTitle);
	}

	async function save(): Promise<boolean> {
		if (deleted || conflict) return false;
		if (savePromise) {
			saveAgain = true;
			return savePromise;
		}
		if (currentContent === savedContent) return true;

		saving = true;
		savePromise = (async () => {
			let ok = false;
			do {
				saveAgain = false;
				ok = await saveOnce();
				if (!saveAgain) break;
				if (conflict || deleted || error || currentContent === savedContent) break;
			} while (true);
			return ok;
		})();

		try {
			return await savePromise;
		} finally {
			savePromise = null;
			saving = false;
		}
	}

	async function saveOnce(): Promise<boolean> {
		const target = currentContent;
		const base = etag;
		error = '';
		try {
			const next = await api.updateNote(fetch, note.id, target, base);
			const split = splitFrontmatter(target);
			note = next;
			etag = next.etag;
			if (!frontmatterId(split.frontmatter) && next.frontmatter?.id) {
				const id = String(next.frontmatter.id);
				// Preserve metadata edits made while this save was in flight.
				frontmatter = ensureFrontmatterID(frontmatter, id);
				savedContent = joinFrontmatter(ensureFrontmatterID(split.frontmatter, id), split.body);
			} else {
				savedContent = target;
			}
			remote = null;
			return true;
		} catch (e) {
			if (e instanceof ApiError && e.status === 412) {
				try {
					const server = await api.noteById(fetch, note.id);
					conflict = { server, local: currentContent };
					etag = server.etag;
				} catch (fetchError) {
					if (fetchError instanceof ApiError && fetchError.status === 404) deleted = true;
					else error = 'The note changed, but the server version could not be loaded.';
				}
			} else if (e instanceof ApiError && e.status === 404) {
				deleted = true;
			} else {
				error = e instanceof ApiError ? e.message : 'Save failed.';
			}
			return false;
		}
	}

	async function saveNow() {
		const ok = await save();
		if (ok && !dirty) toast('Saved.');
	}

	async function overwrite() {
		if (!conflict || saving) return;
		const target = currentContent;
		saving = true;
		error = '';
		try {
			const next = await api.updateNote(fetch, note.id, target, conflict.server.etag);
			note = next;
			etag = next.etag;
			savedContent = target;
			conflict = null;
			remote = null;
			toast('Overwrote the server version.');
		} catch (e) {
			if (e instanceof ApiError && e.status === 412) {
				try {
					conflict = { server: await api.noteById(fetch, note.id), local: currentContent };
					error = 'The note changed again. Review the new difference.';
				} catch {
					error = 'The note changed again, but the server version could not be reloaded.';
				}
			} else if (e instanceof ApiError && e.status === 404) {
				deleted = true;
			} else {
				error = e instanceof ApiError ? e.message : 'Overwrite failed.';
			}
		} finally {
			saving = false;
		}
	}

	function reloadServer() {
		if (!conflict) return;
		if (dirty && !window.confirm('Discard your unsaved changes and load the server version?')) return;
		applyNote(conflict.server);
		toast('Loaded the server version.');
	}

	function reloadRemote() {
		if (!remote) return;
		if (dirty && !window.confirm('Discard your unsaved changes and load the server version?')) return;
		applyNote(remote);
	}

	function reviewRemote() {
		if (!remote) return;
		conflict = { server: remote, local: currentContent };
		remote = null;
	}

	async function refreshFromServer() {
		try {
			const next = await api.noteById(fetch, note.id);
			if (next.etag === etag && next.path === note.path) return;
			if (dirty) {
				remote = next;
				return;
			}
			const previousPath = note.path;
			applyNote(next);
			if (next.path !== previousPath) {
				await goto(noteHref(next.path), { replaceState: true });
			}
		} catch (e) {
			if (e instanceof ApiError && e.status === 404) deleted = true;
		}
	}

	beforeNavigate((navigation) => {
		if (!dirty || !navigation.to) return;
		if (navigation.to.url.pathname === page.url.pathname) return;
		// Signing out or an expired-session redirect must not trap the user here.
		if (navigation.to.url.pathname === '/login' || navigation.to.url.pathname === '/setup') return;
		const target = navigation.to.url.href;
		navigation.cancel();
		void (async () => {
			const ok = await save();
			if (!ok || dirty || conflict) {
				toast('Save failed. Staying on this note.', 'error');
				return;
			}
			await goto(target);
		})();
	});

	function resizeTitle() {
		if (!titleEl) return;
		titleEl.style.height = 'auto';
		titleEl.style.height = `${titleEl.scrollHeight}px`;
	}

	function onTitleInput(event: Event) {
		const value = (event.currentTarget as HTMLTextAreaElement).value;
		title = value;
		frontmatter = setTitle(frontmatter, value);
		resizeTitle();
	}

	function onTitleFocus() {
		titleAtFocus = title;
	}

	async function onTitleBlur(event: FocusEvent) {
		if (title === titleAtFocus) return;
		// A link click is a navigation; beforeNavigate owns the save. Starting a
		// competing file rename here would race that navigation.
		if (event.relatedTarget instanceof Element && event.relatedTarget.closest('a')) {
			titleAtFocus = title;
			return;
		}
		await commitTitleRename();
	}

	function onTitleKeydown(event: KeyboardEvent) {
		if (event.key === 'Enter' && !event.shiftKey) {
			event.preventDefault();
			(event.currentTarget as HTMLTextAreaElement).blur();
		}
	}

	async function commitTitleRename() {
		if (titleRenameBusy || deleted || conflict) return;
		const nextTitle = title.trim();
		if (!nextTitle) return;
		const target = notePathForTitle(nextTitle, note.path);
		titleRenameBusy = true;
		try {
			const ok = await save();
			if (!ok || dirty || conflict) return;
			if (target === note.path) {
				titleAtFocus = title;
				return;
			}
			const result = await api.moveNote(fetch, note.id, target, true);
			applyNote(result.note);
			titleAtFocus = title;
			const rewritten = result.links_rewritten
				? ` ${result.links_rewritten} note${result.links_rewritten === 1 ? '' : 's'} had links rewritten.`
				: '';
			toast(`Renamed to ${result.note.path}.${rewritten}`);
			await goto(noteHref(result.note.path), { replaceState: true });
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'The title was saved, but the file could not be renamed.', 'error');
		} finally {
			titleRenameBusy = false;
		}
	}

	function addTag() {
		const tag = tagDraft.trim().replace(/^#+/, '');
		if (!tag) return;
		frontmatter = setTags(frontmatter, [...tags, tag]);
		tagDraft = '';
		tagInputOpen = false;
	}

	function removeTag(tag: string) {
		frontmatter = setTags(
			frontmatter,
			tags.filter((current) => current !== tag)
		);
	}

	function applyProperties(next: Record<string, unknown>) {
		frontmatter = replaceCustomProperties(frontmatter, next);
	}

	function openGhost(target: string) {
		ghostTarget = target;
		createOpen = true;
	}

	function onGhostCreated(created: Note) {
		ghostTarget = '';
		createOpen = false;
		toast(`Created ${created.path}.`);
		void refreshFromServer();
	}

	function onWikilinkClick(link: WikilinkClick) {
		if (link.kind === 'embed' && isImageTarget(link.target)) {
			window.open(fileHref(link.target), '_blank', 'noopener,noreferrer');
			return;
		}
		const match = note.links.find(
			(candidate) =>
				candidate.kind === link.kind &&
				candidate.target.toLowerCase() === link.target.toLowerCase()
		);
		if (match && (match.resolution === 'exact' || match.resolution === 'suffix') && match.path) {
			const fragment = link.fragment ? `#${encodeURIComponent(link.fragment)}` : '';
			void goto(noteHref(match.path) + fragment);
			return;
		}
		if (match?.resolution === 'ambiguous') {
			toast('That link matches more than one note. See the unresolved links below.', 'error');
			return;
		}
		openGhost(link.target);
	}

	function onLinkClick(href: string) {
		if (!href) return;
		if (/^(https?:|mailto:)/i.test(href)) {
			window.open(href, '_blank', 'noopener,noreferrer');
			return;
		}
		if (href.startsWith('#')) return;
		if (href.startsWith('/')) {
			void goto(href);
			return;
		}
		window.open(href, '_blank', 'noopener,noreferrer');
	}
</script>

<svelte:head><title>{displayTitle} · lewnotes</title></svelte:head>

<article class="flex min-h-[calc(100dvh_-_3rem)] flex-col sm:min-h-[calc(100dvh_-_5rem)]">
	<div class="mb-5 flex flex-wrap items-center justify-between gap-2 text-xs">
		<Badge variant={saving || !dirty ? 'secondary' : 'outline'} class="font-normal">
			{saving ? 'Saving…' : dirty ? 'Unsaved changes' : 'Saved'}
		</Badge>
		<div class="flex items-center gap-2">
			<NoteProperties
				bind:open={propertiesOpen}
				{properties}
				path={note.path}
				onapply={applyProperties}
				disabled={deleted || !!conflict}
			/>
			<Button
				variant="outline"
				size="sm"
				disabled={!dirty || saving || !!conflict || deleted}
				onclick={() => void saveNow()}>Save</Button
			>
		</div>
	</div>

	{#if deleted}
		<Alert.Root class="mb-6 border-amber-400/40 bg-amber-950/20">
			<Alert.Title class="text-amber-100">This note is no longer in the vault.</Alert.Title>
			<Alert.Description class="text-amber-200/80">
				It was moved to the trash or deleted elsewhere. The last loaded text remains below for
				reference.
			</Alert.Description>
			<Button href="/notes" variant="outline" size="sm" class="mt-3 w-fit">Back to notes</Button>
		</Alert.Root>
	{/if}

	{#if remote && dirty}
		<Alert.Root class="mb-6 border-amber-400/40 bg-amber-950/20" role="status">
			<Alert.Title class="text-amber-100">This note changed on the server.</Alert.Title>
			<Alert.Description class="text-amber-200/80">
				Your edits are still here. Review the difference or reload the server version.
			</Alert.Description>
			<div class="mt-3 flex flex-wrap gap-2">
				<Button variant="outline" size="sm" onclick={reviewRemote}>Review difference</Button>
				<Button variant="outline" size="sm" onclick={reloadRemote}>Reload server version</Button>
			</div>
		</Alert.Root>
	{/if}

	{#if error}
		<Alert.Root variant="destructive" class="mb-6">
			<Alert.Title>Save problem</Alert.Title>
			<Alert.Description>{error}</Alert.Description>
			<Button
				variant="ghost"
				size="icon-xs"
				class="absolute top-2 right-2"
				aria-label="Dismiss error"
				onclick={() => (error = '')}
			>
				<XIcon />
			</Button>
		</Alert.Root>
	{/if}

	{#if conflict}
		<ConflictDiff
			server={conflict.server}
			mine={currentContent}
			busy={saving}
			onreload={reloadServer}
			onoverwrite={overwrite}
		/>
	{/if}

	<Textarea
		bind:ref={titleEl}
		class="mb-2 min-h-0 resize-none overflow-hidden rounded-none border-0 bg-transparent p-0 text-3xl font-semibold tracking-tight text-neutral-100 shadow-none placeholder:text-neutral-600 focus-visible:ring-0 sm:text-4xl"
		value={title}
		oninput={onTitleInput}
		onfocus={onTitleFocus}
		onblur={(event) => void onTitleBlur(event)}
		onkeydown={onTitleKeydown}
		rows={1}
		placeholder="Untitled"
		spellcheck="true"
		aria-label="Note title"
		disabled={deleted || !!conflict}
	/>

	<div class="mb-7 flex flex-wrap items-center gap-x-3 gap-y-2 text-xs text-muted-foreground">
		<span>Updated {formatDate(note.updated_at)}</span>
		<span>{note.links.length} link{note.links.length === 1 ? '' : 's'}</span>
		<span>{note.backlinks.length} backlink{note.backlinks.length === 1 ? '' : 's'}</span>
		{#each tags as tag (tag)}
			<Badge variant="secondary" class="gap-1 pr-1 font-normal">
				#{tag}
				<Button
					variant="ghost"
					size="icon-xs"
					class="size-4 p-0"
					aria-label={`Remove tag ${tag}`}
					disabled={deleted || !!conflict}
					onclick={() => removeTag(tag)}
				>
					<XIcon />
				</Button>
			</Badge>
		{/each}
		{#if tagInputOpen}
			<form
				class="inline-flex items-center gap-1"
				onsubmit={(event) => {
					event.preventDefault();
					addTag();
				}}
			>
				<Input
					class="h-7 w-24 text-[11px]"
					aria-label="New tag"
					bind:value={tagDraft}
					placeholder="tag"
					autocomplete="off"
				/>
				<Button size="xs" type="submit">Add</Button>
			</form>
		{:else}
			<Button
				variant="ghost"
				size="xs"
				disabled={deleted || !!conflict}
				onclick={() => (tagInputOpen = true)}>+ Tag</Button
			>
		{/if}
	</div>

	<MarkdownEditor
		value={body}
		onchange={(next) => (body = next)}
		onsave={() => void saveNow()}
		onwikilink={onWikilinkClick}
		onlink={onLinkClick}
		getLinkTargets={() => notePaths(getVaultTree())}
		readonly={deleted || !!conflict}
	/>

	<p class="mt-3 text-[11px] text-muted-foreground">
		Autosaves after a pause. ⌘S / Ctrl-S saves now. Type on a line to reveal its markdown. ⌘-click / Ctrl-click
		links to open them.
	</p>

	{#if issues.length}
		<section class="border-border-subtle mt-12 border-t pt-6">
			<h2 class="text-xs font-medium tracking-[0.14em] text-muted-foreground uppercase">Unresolved links</h2>
			<ul class="mt-3 space-y-2">
				{#each issues as link (`${link.kind}:${link.target}:${link.line}`)}
					<li class="border-border-subtle rounded border px-3 py-2">
						<div class="flex flex-wrap items-center justify-between gap-2">
							<span class="font-mono text-xs text-neutral-400">
								{link.kind === 'embed' ? '!' : ''}{link.target}{#if link.fragment}#{link.fragment}{/if}
								<span class="ml-1 text-[10px] uppercase text-muted-foreground">{link.resolution}</span>
							</span>
							{#if link.resolution === 'unresolved' && !(link.kind === 'embed' && isImageTarget(link.target))}
								<Button variant="outline" size="xs" onclick={() => openGhost(link.target)}>
									Create note
								</Button>
							{:else if link.resolution === 'unresolved'}
								<span class="text-[11px] text-muted-foreground">Attachment embeds arrive with the attachments slice.</span>
							{/if}
						</div>
						{#if link.resolution === 'ambiguous' && link.candidates?.length}
							<ul class="mt-2 flex flex-wrap gap-2">
								{#each link.candidates as candidate (candidate)}
									<li>
										<Button
											variant="ghost"
											size="xs"
											class="h-auto px-1 py-0.5 font-mono text-[11px]"
											href={noteHref(candidate)}>{candidate}</Button
										>
									</li>
								{/each}
							</ul>
						{/if}
					</li>
				{/each}
			</ul>
		</section>
	{/if}

	{#if note.backlinks.length}
		<section class="border-border-subtle mt-10 border-t pt-6">
			<h2 class="text-xs font-medium tracking-[0.14em] text-muted-foreground uppercase">Linked from</h2>
			<ul class="mt-3 space-y-1">
				{#each note.backlinks as link (link.id + ':' + link.line)}
					<li>
						<a class="text-sm text-neutral-300 underline decoration-neutral-700 underline-offset-4 hover:text-neutral-100" href={noteHref(link.path)}>
							{link.title || link.path}
						</a>
						{#if link.alias}
							<span class="ml-2 text-xs text-muted-foreground">as “{link.alias}”</span>
						{:else if link.fragment}
							<span class="ml-2 text-xs text-muted-foreground">#{link.fragment}</span>
						{/if}
						<span class="ml-2 font-mono text-[10px] text-muted-foreground">line {link.line}</span>
					</li>
				{/each}
			</ul>
		</section>
	{/if}
</article>

<CreateNoteDialog
	bind:open={createOpen}
	initialPath={ghostPath}
	initialTitle={ghostTitle}
	oncreated={onGhostCreated}
/>
