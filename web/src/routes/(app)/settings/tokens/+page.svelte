<script lang="ts">
	import { onMount } from 'svelte';

	import { api, ApiError, formatDate, type IssuedToken, type TokenView } from '#lib/api';
	import { toast } from '#lib/toast';
	import { Button, buttonVariants } from '$lib/components/ui/button/index.js';
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import * as Empty from '$lib/components/ui/empty/index.js';
	import * as Field from '$lib/components/ui/field/index.js';
	import { Input } from '$lib/components/ui/input/index.js';

	let tokens = $state<TokenView[]>([]);
	let loading = $state(true);
	let loadError = $state('');

	let createOpen = $state(false);
	let name = $state('');
	let creating = $state(false);
	let createError = $state('');

	let issued = $state<IssuedToken | null>(null);
	let tokenOpen = $state(false);
	let copied = $state(false);

	let revoking = $state<TokenView | null>(null);
	let revokeOpen = $state(false);
	let revokeBusy = $state(false);

	onMount(refresh);

	async function refresh() {
		loading = true;
		loadError = '';
		try {
			const result = await api.listTokens(fetch);
			tokens = result.tokens;
		} catch (error) {
			loadError = error instanceof ApiError ? error.message : 'Could not load device tokens.';
		} finally {
			loading = false;
		}
	}

	function openCreate() {
		name = '';
		createError = '';
		createOpen = true;
	}

	async function createToken(event: SubmitEvent) {
		event.preventDefault();
		if (creating) return;
		creating = true;
		createError = '';
		try {
			const token = await api.createToken(fetch, name.trim());
			tokens = [token, ...tokens];
			createOpen = false;
			issued = token;
			copied = false;
			tokenOpen = true;
		} catch (error) {
			createError = error instanceof ApiError ? error.message : 'Could not create the token.';
		} finally {
			creating = false;
		}
	}

	async function copyToken() {
		if (!issued) return;
		try {
			await navigator.clipboard.writeText(issued.token);
			copied = true;
			toast('Token copied.');
		} catch {
			toast('Copy failed. Select the token and copy it manually.', 'error');
		}
	}

	function openRevoke(token: TokenView) {
		revoking = token;
		revokeOpen = true;
	}

	async function revokeToken() {
		if (!revoking || revokeBusy) return;
		revokeBusy = true;
		try {
			await api.revokeToken(fetch, revoking.id);
			tokens = tokens.filter((token) => token.id !== revoking?.id);
			toast(`Revoked ${revoking.name}.`);
			revokeOpen = false;
			revoking = null;
		} catch (error) {
			toast(error instanceof ApiError ? error.message : 'Could not revoke the token.', 'error');
		} finally {
			revokeBusy = false;
		}
	}
</script>

<svelte:head><title>Device tokens · lewnotes</title></svelte:head>

<header class="mb-8 flex flex-wrap items-end justify-between gap-3">
	<div>
		<p class="text-muted-foreground text-xs tracking-[0.18em] uppercase">settings</p>
		<h1 class="mt-1 text-3xl font-semibold tracking-tight">Device tokens</h1>
		<p class="text-muted-foreground mt-2 max-w-2xl text-sm">
			Long-lived credentials for the CLI, TUI, and agents. The usual flow is
			<code class="font-mono text-xs">lewnotes login</code>, which creates one of these after you
			approve the device. Revoke a token here if it leaks or a device is retired.
		</p>
	</div>
	<Button onclick={openCreate}>New token</Button>
</header>

{#if loading}
	<p class="text-muted-foreground text-sm">Loading tokens…</p>
{:else if loadError}
	<p class="text-destructive text-sm" role="alert">{loadError}</p>
	<Button variant="outline" class="mt-3" onclick={refresh}>Retry</Button>
{:else if tokens.length === 0}
	<Empty.Root class="border-border bg-card/40 border">
		<Empty.Header>
			<Empty.Title>No device tokens yet</Empty.Title>
			<Empty.Description>
				Run <code class="font-mono text-xs">lewnotes login</code> on a machine and approve it in the
				browser; the token appears here. Agents can also use one through
				<code class="font-mono text-xs">LEWNOTES_TOKEN</code>.
			</Empty.Description>
		</Empty.Header>
	</Empty.Root>
{:else}
	<ul class="border-border divide-border divide-y overflow-hidden rounded-lg border">
		{#each tokens as token (token.id)}
			<li class="flex flex-wrap items-start justify-between gap-4 p-4">
				<div class="min-w-0 space-y-1">
					<p class="font-medium">{token.name}</p>
					<p class="text-muted-foreground font-mono text-[11px]">{token.prefix}</p>
					<p class="text-muted-foreground text-xs">
						Created {formatDate(token.created_at)}
						{#if token.last_used_at}
							· last used {formatDate(token.last_used_at)}
						{:else}
							· never used
						{/if}
						{#if token.user_agent}
							· {token.user_agent}
						{/if}
					</p>
				</div>
				<Button variant="outline" size="sm" onclick={() => openRevoke(token)}>Revoke</Button>
			</li>
		{/each}
	</ul>
{/if}

<Dialog.Root bind:open={createOpen}>
	<Dialog.Content class="sm:max-w-md">
		<form class="space-y-5" onsubmit={createToken}>
			<Dialog.Header>
				<Dialog.Title>New device token</Dialog.Title>
				<Dialog.Description>
					Name it after the device or agent that will use it. The token is shown once and cannot be
					recovered.
				</Dialog.Description>
			</Dialog.Header>

			<Field.Field>
				<Field.Label for="token-name">Name</Field.Label>
				<Input
					id="token-name"
					bind:value={name}
					required
					maxlength={100}
					placeholder="caleb-laptop"
					autocomplete="off"
					spellcheck="false"
				/>
			</Field.Field>

			{#if createError}
				<p class="text-destructive text-sm" role="alert">{createError}</p>
			{/if}

			<Dialog.Footer>
				<Dialog.Close class={buttonVariants({ variant: 'outline' })}>Cancel</Dialog.Close>
				<Button type="submit" disabled={creating}>
					{creating ? 'Creating…' : 'Create token'}
				</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>

<Dialog.Root bind:open={tokenOpen}>
	<Dialog.Content class="sm:max-w-lg">
		<Dialog.Header>
			<Dialog.Title>Copy this token now</Dialog.Title>
			<Dialog.Description>
				Use it with <code class="font-mono text-xs">lewnotes login --token-stdin</code> or set
				<code class="font-mono text-xs">LEWNOTES_TOKEN</code>. You will not be able to see it again.
			</Dialog.Description>
		</Dialog.Header>

		{#if issued}
			<div class="space-y-3">
				<Input class="font-mono text-[12px]" readonly value={issued.token} onclick={(event) => event.currentTarget.select()} />
				<p class="text-muted-foreground text-xs">
					Treat it like a password. Agents should read it from <code class="font-mono">LEWNOTES_TOKEN</code>
					rather than embedding it in prompts or files.
				</p>
			</div>
		{/if}

		<Dialog.Footer>
			<Button variant="outline" onclick={copyToken}>{copied ? 'Copied' : 'Copy token'}</Button>
			<Dialog.Close class={buttonVariants()}>Done</Dialog.Close>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>

<Dialog.Root bind:open={revokeOpen}>
	<Dialog.Content class="sm:max-w-md">
		<Dialog.Header>
			<Dialog.Title>Revoke {revoking?.name}?</Dialog.Title>
			<Dialog.Description>
				Any client using this token will be signed out immediately. This cannot be undone.
			</Dialog.Description>
		</Dialog.Header>
		<Dialog.Footer>
			<Dialog.Close class={buttonVariants({ variant: 'outline' })}>Cancel</Dialog.Close>
			<Button variant="destructive" disabled={revokeBusy} onclick={revokeToken}>
				{revokeBusy ? 'Revoking…' : 'Revoke token'}
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
