<script lang="ts">
	import { onMount } from 'svelte';

	import { api, ApiError, type InstanceSettings, type ProbeResult } from '#lib/api';
	import { toast } from '#lib/toast';
	import * as Alert from '$lib/components/ui/alert/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as Field from '$lib/components/ui/field/index.js';
	import { Input } from '$lib/components/ui/input/index.js';

	let settings = $state<InstanceSettings | null>(null);
	let draft = $state('');
	let loading = $state(true);
	let loadError = $state('');
	let testing = $state(false);
	let saving = $state(false);
	let actionError = $state('');
	let probe = $state<ProbeResult | null>(null);
	let copied = $state(false);

	const fromConfig = $derived(settings?.source === 'config');
	const dirty = $derived(settings !== null && draft.replace(/\/+$/, '') !== settings.public_url);

	onMount(load);

	async function load() {
		loading = true;
		loadError = '';
		try {
			settings = await api.getSettings(fetch);
			draft = settings.public_url;
		} catch (error) {
			loadError = error instanceof ApiError ? error.message : 'Could not load instance settings.';
		} finally {
			loading = false;
		}
	}

	function useCurrentOrigin() {
		draft = window.location.origin;
		probe = null;
		actionError = '';
	}

	async function testConnection(): Promise<ProbeResult | null> {
		testing = true;
		actionError = '';
		probe = null;
		try {
			probe = await api.validateServer(fetch, draft.trim());
			return probe;
		} catch (error) {
			actionError =
				error instanceof ApiError ? error.message : 'Could not reach that address.';
			return null;
		} finally {
			testing = false;
		}
	}

	async function validateAndSave() {
		if (!settings || saving) return;
		saving = true;
		actionError = '';
		try {
			const checked = await testConnection();
			if (!checked) return;
			settings = await api.updateSettings(fetch, draft.trim());
			draft = settings.public_url;
			toast('Public URL saved.');
		} catch (error) {
			actionError = error instanceof ApiError ? error.message : 'Could not save the setting.';
		} finally {
			saving = false;
		}
	}

	async function copyCommand() {
		if (!settings) return;
		try {
			await navigator.clipboard.writeText(`lewnotes login ${settings.public_url}`);
			copied = true;
			toast('Command copied.');
		} catch {
			toast('Copy failed. Select the command and copy it manually.', 'error');
		}
	}

	function sourceLabel(source: InstanceSettings['source']): string {
		switch (source) {
			case 'config':
				return 'Set by server.public_url in config.toml (read-only here)';
			case 'settings':
				return 'Saved from this page';
			default:
				return 'Derived from the current request — set this before using the CLI from another machine';
		}
	}
</script>

<svelte:head><title>Instance · lewnotes</title></svelte:head>

<header class="mb-8">
	<p class="text-muted-foreground text-xs tracking-[0.18em] uppercase">settings</p>
	<h1 class="mt-1 text-3xl font-semibold tracking-tight">Instance</h1>
	<p class="text-muted-foreground mt-2 max-w-2xl text-sm">
		The address people use to reach this server. It is baked into device tokens, login links, and
		the command shown to connect a machine.
	</p>
</header>

{#if loading}
	<p class="text-muted-foreground text-sm">Loading settings…</p>
{:else if loadError}
	<p class="text-destructive text-sm" role="alert">{loadError}</p>
	<Button variant="outline" class="mt-3" onclick={load}>Retry</Button>
{:else if settings}
	<div class="space-y-8">
		{#if settings.source === 'derived'}
			<Alert.Root>
				<Alert.Title>No public URL is set</Alert.Title>
				<Alert.Description>
					Tokens and login links currently use whatever host the request arrived on
					(<code class="font-mono text-xs">{settings.derived_origin}</code>). That is fine for
					testing on this machine, but a CLI elsewhere needs the address you actually use.
				</Alert.Description>
			</Alert.Root>
		{/if}

		{#if settings.public_url !== settings.derived_origin}
			<Alert.Root>
				<Alert.Title>Tokens will point somewhere else</Alert.Title>
				<Alert.Description>
					You are browsing {settings.derived_origin}, but tokens and login links will use
					{settings.public_url}.
				</Alert.Description>
			</Alert.Root>
		{/if}

		<form class="space-y-5" onsubmit={(event) => { event.preventDefault(); void validateAndSave(); }}>
			<Field.Field>
				<Field.Label for="public-url">Public URL</Field.Label>
				<div class="flex flex-col gap-2 sm:flex-row">
					<Input
						id="public-url"
						bind:value={draft}
						disabled={fromConfig}
						placeholder="https://notes.example.com"
						autocomplete="url"
						spellcheck="false"
						class="font-mono text-sm sm:flex-1"
					/>
					{#if !fromConfig}
						<Button type="button" variant="outline" onclick={useCurrentOrigin}>
							Use {settings.derived_origin}
						</Button>
					{/if}
				</div>
				<Field.Description>{sourceLabel(settings.source)}</Field.Description>
			</Field.Field>

			{#if probe}
				<div
					class="border-border bg-card/40 rounded-lg border p-4 text-sm"
					role="status"
					aria-live="polite"
				>
					<p class="font-medium text-emerald-400">
						Reachable — lewnotes {probe.version || 'unknown version'}
						<span class="text-muted-foreground font-normal"
							>({probe.latency_ms.toFixed(0)} ms)</span
						>
					</p>
					<p class="text-muted-foreground mt-1 text-xs">
						{probe.instance_name ? `Instance “${probe.instance_name}”. ` : ''}
						{probe.setup_required
							? 'It still needs its owner account before a CLI can sign in.'
							: 'It answered like a lewnotes server and is ready for a login.'}
					</p>
				</div>
			{/if}

			{#if actionError}
				<Alert.Root variant="destructive">
					<Alert.Title>Could not use that address</Alert.Title>
					<Alert.Description>{actionError}</Alert.Description>
				</Alert.Root>
			{/if}

			{#if !fromConfig}
				<div class="flex flex-wrap gap-2">
					<Button type="submit" disabled={saving || testing || !dirty && probe !== null}>
						{saving ? 'Checking…' : 'Test and save'}
					</Button>
					<Button
						type="button"
						variant="outline"
						disabled={testing || saving || draft.trim() === ''}
						onclick={() => void testConnection()}
					>
						{testing ? 'Checking…' : 'Test only'}
					</Button>
				</div>
			{/if}
		</form>

		<section class="border-border rounded-lg border p-5">
			<h2 class="font-semibold tracking-tight">Connect a machine</h2>
			<p class="text-muted-foreground mt-1 text-sm">
				Run this on the machine that will use the vault. It opens a browser here, you approve the
				device, and the CLI receives its token encrypted to that machine — nothing to copy.
			</p>
			<div class="mt-3 flex flex-col gap-2 sm:flex-row">
				<Input
					readonly
					class="font-mono text-xs sm:flex-1"
					value={`lewnotes login ${settings.public_url}`}
					onclick={(event) => event.currentTarget.select()}
				/>
				<Button variant="outline" onclick={copyCommand}>{copied ? 'Copied' : 'Copy'}</Button>
			</div>
			<p class="text-muted-foreground mt-3 text-xs">
				No browser on that machine? Add <code class="font-mono">--no-browser</code>; the CLI prints a
				short code to approve here. Agents and CI should use
				<code class="font-mono">LEWNOTES_TOKEN</code> with a token from the
				<a class="underline underline-offset-4" href="/settings/tokens">tokens page</a>.
			</p>
		</section>

		<section class="border-border rounded-lg border p-5">
			<h2 class="font-semibold tracking-tight">Server</h2>
			<dl class="mt-3 grid gap-2 text-sm sm:grid-cols-2">
				<div>
					<dt class="text-muted-foreground text-xs tracking-wide uppercase">Listening on</dt>
					<dd class="font-mono text-xs">{settings.listen || 'unknown'}</dd>
				</div>
				<div>
					<dt class="text-muted-foreground text-xs tracking-wide uppercase">Trust proxy headers</dt>
					<dd class="font-mono text-xs">{settings.trust_proxy ? 'yes' : 'no'}</dd>
				</div>
			</dl>
			<p class="text-muted-foreground mt-3 text-xs">
				If the address above is behind a reverse proxy, enable
				<code class="font-mono">server.trust_proxy</code> in config.toml so the server sees the
				original scheme and host.
			</p>
		</section>
	</div>
{/if}
