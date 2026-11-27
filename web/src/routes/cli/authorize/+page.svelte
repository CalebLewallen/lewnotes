<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { onMount } from 'svelte';

	import { api, ApiError, setCSRF, type CLILoginPreview } from '#lib/api';
	import * as Alert from '$lib/components/ui/alert/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as Field from '$lib/components/ui/field/index.js';
	import { Input } from '$lib/components/ui/input/index.js';

	const id = $derived(page.url.searchParams.get('id') ?? '');

	let preview = $state<CLILoginPreview | null>(null);
	let deviceName = $state('');
	let loading = $state(true);
	let error = $state('');
	let busy = $state<'approve' | 'deny' | null>(null);
	let outcome = $state<'approved' | 'denied' | null>(null);
	let now = $state(Date.now());
	let sameOrigin = $state(true);

	onMount(async () => {
		try {
			const me = await api.me(fetch);
			setCSRF(me.csrf_token);
		} catch {
			await goto(`/login?next=${encodeURIComponent(`/cli/authorize?id=${id}`)}`);
			return;
		}
		if (!id) {
			error = 'This link is missing its request id. Run the login command again.';
			loading = false;
			return;
		}
		try {
			preview = await api.previewCLILogin(fetch, id);
			deviceName = preview.device_name;
			sameOrigin = preview.server === window.location.origin;
		} catch (e) {
			error =
				e instanceof ApiError
					? e.message
					: 'Could not load this login request. It may have expired.';
		} finally {
			loading = false;
		}
	});

	$effect(() => {
		if (!preview) return;
		const timer = setInterval(() => (now = Date.now()), 1000);
		return () => clearInterval(timer);
	});

	const remaining = $derived.by(() => {
		if (!preview) return '';
		const ms = new Date(preview.expires_at).getTime() - now;
		if (ms <= 0) return 'expired';
		const total = Math.floor(ms / 1000);
		return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, '0')}`;
	});

	async function approve() {
		if (!preview || busy) return;
		busy = 'approve';
		error = '';
		try {
			const result = await api.approveCLILogin(fetch, preview.id, deviceName.trim());
			if (result.redirect_url) {
				window.location.assign(result.redirect_url);
				return;
			}
			outcome = 'approved';
		} catch (e) {
			error = e instanceof ApiError ? e.message : 'Could not approve this device.';
		} finally {
			busy = null;
		}
	}

	async function deny() {
		if (!preview || busy) return;
		busy = 'deny';
		error = '';
		try {
			await api.denyCLILogin(fetch, preview.id);
			outcome = 'denied';
		} catch (e) {
			error = e instanceof ApiError ? e.message : 'Could not deny this device.';
		} finally {
			busy = null;
		}
	}
</script>

<svelte:head><title>Authorize device · lewnotes</title></svelte:head>

<main class="flex min-h-dvh items-center justify-center px-6 py-16">
	<section class="w-full max-w-lg space-y-6">
		<header class="space-y-2">
			<p class="text-muted-foreground text-xs tracking-[0.2em] uppercase">cli login</p>
			<h1 class="text-3xl font-semibold tracking-tight">Authorize a device</h1>
		</header>

		{#if loading}
			<p class="text-muted-foreground text-sm">Loading the request…</p>
		{:else if error && !preview}
			<Alert.Root variant="destructive">
				<Alert.Title>Cannot show this request</Alert.Title>
				<Alert.Description>{error}</Alert.Description>
			</Alert.Root>
			<Button variant="outline" onclick={() => goto('/cli')}>Enter a code instead</Button>
		{:else if outcome === 'approved'}
			<Alert.Root>
				<Alert.Title>Device approved</Alert.Title>
				<Alert.Description>
					The CLI should finish in a second or two. You can close this tab.
				</Alert.Description>
			</Alert.Root>
		{:else if outcome === 'denied'}
			<Alert.Root>
				<Alert.Title>Device denied</Alert.Title>
				<Alert.Description>
					No token was issued. The CLI will stop; run the command again if this was a mistake.
				</Alert.Description>
			</Alert.Root>
		{:else if preview}
			{#if preview.status !== 'pending'}
				<Alert.Root variant="destructive">
					<Alert.Title>This request is {preview.status}</Alert.Title>
					<Alert.Description>
						Run <code class="font-mono text-xs">lewnotes login</code> again to start a fresh one.
					</Alert.Description>
				</Alert.Root>
			{:else}
				<div class="border-border divide-border divide-y rounded-lg border">
					<div class="flex items-start justify-between gap-4 p-4">
						<div>
							<p class="text-muted-foreground text-xs tracking-wide uppercase">Device</p>
							<p class="mt-1 font-medium">{preview.device_name}</p>
						</div>
						<div class="text-right">
							<p class="text-muted-foreground text-xs tracking-wide uppercase">Expires</p>
							<p class="mt-1 font-mono text-sm">{remaining}</p>
						</div>
					</div>
					<dl class="grid gap-3 p-4 text-sm sm:grid-cols-2">
						<div>
							<dt class="text-muted-foreground text-xs tracking-wide uppercase">Requested by</dt>
							<dd class="mt-1 font-mono text-xs">{preview.client}</dd>
						</div>
						<div>
							<dt class="text-muted-foreground text-xs tracking-wide uppercase">Server</dt>
							<dd class="mt-1 font-mono text-xs">{preview.server}</dd>
						</div>
					</dl>
				</div>

				{#if !sameOrigin}
					<Alert.Root variant="destructive">
						<Alert.Title>Wrong server?</Alert.Title>
						<Alert.Description>
							The CLI asked for <code class="font-mono text-xs">{preview.server}</code>, but you are
							signed in at <code class="font-mono text-xs">{window.location.origin}</code>. Approving
							issues a token for the address the CLI used, so only continue if that is what you
							expect.
						</Alert.Description>
					</Alert.Root>
				{/if}

				<Field.Field>
					<Field.Label for="device-name">Device name</Field.Label>
					<Input
						id="device-name"
						bind:value={deviceName}
						maxlength={100}
						autocomplete="off"
						spellcheck="false"
					/>
					<Field.Description>
						Shown later on the tokens page so you can recognize and revoke it.
					</Field.Description>
				</Field.Field>

				{#if error}
					<Alert.Root variant="destructive">
						<Alert.Title>Could not finish</Alert.Title>
						<Alert.Description>{error}</Alert.Description>
					</Alert.Root>
				{/if}

				<div class="flex flex-wrap gap-2">
					<Button onclick={approve} disabled={busy !== null || !deviceName.trim()}>
						{busy === 'approve' ? 'Authorizing…' : 'Authorize device'}
					</Button>
					<Button variant="outline" onclick={deny} disabled={busy !== null}>
						{busy === 'deny' ? 'Denying…' : 'Deny'}
					</Button>
				</div>
			{/if}
		{/if}

		<p class="text-muted-foreground text-xs leading-relaxed">
			This page never displays the token. The server encrypts it to the waiting CLI process, and the
			browser only passes along the request id.
		</p>
	</section>
</main>
