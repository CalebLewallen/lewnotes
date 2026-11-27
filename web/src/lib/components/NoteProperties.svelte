<script lang="ts">
	import PlusIcon from '@lucide/svelte/icons/plus';
	import XIcon from '@lucide/svelte/icons/x';
	import { tick } from 'svelte';

	import { Button, buttonVariants } from '$lib/components/ui/button/index.js';
	import { Checkbox } from '$lib/components/ui/checkbox/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { NativeSelect } from '$lib/components/ui/native-select/index.js';
	import * as Popover from '$lib/components/ui/popover/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';

	interface Props {
		open?: boolean;
		disabled?: boolean;
		properties: Record<string, unknown>;
		path: string;
		onapply: (properties: Record<string, unknown>) => void;
	}

	type Kind = 'string' | 'number' | 'boolean' | 'json';

	interface Row {
		key: string;
		kind: Kind;
		value: string;
		boolean: boolean;
	}

	let { open = $bindable(false), disabled = false, properties, path, onapply }: Props = $props();

	let rows = $state<Row[]>([]);
	let error = $state('');
	let wasOpen = false;
	let panel = $state<HTMLDivElement | null>(null);

	$effect(() => {
		if (open && !wasOpen) {
			rows = Object.entries(properties).map(([key, value]) => toRow(key, value));
			error = '';
			void tick().then(() => {
				panel?.querySelector<HTMLElement>('input, select, button')?.focus();
			});
		}
		wasOpen = open;
	});

	function toRow(key: string, value: unknown): Row {
		if (typeof value === 'boolean') return { key, kind: 'boolean', value: '', boolean: value };
		if (typeof value === 'number') return { key, kind: 'number', value: String(value), boolean: false };
		if (value !== null && typeof value === 'object') {
			return { key, kind: 'json', value: JSON.stringify(value, null, 2), boolean: false };
		}
		return { key, kind: 'string', value: value == null ? '' : String(value), boolean: false };
	}

	function addRow() {
		rows = [...rows, { key: '', kind: 'string', value: '', boolean: false }];
	}

	function removeRow(index: number) {
		rows = rows.filter((_, current) => current !== index);
	}

	function apply() {
		const next: Record<string, unknown> = {};
		for (const row of rows) {
			const key = row.key.trim();
			if (!key) continue;
			if (key === 'id' || key === 'title' || key === 'tags') {
				error = 'id, title, and tags are managed by their own fields.';
				return;
			}
			if (key in next) {
				error = `Duplicate property “${key}”.`;
				return;
			}
			try {
				next[key] = parseValue(row);
			} catch {
				error = `“${key}” is not valid ${row.kind === 'json' ? 'JSON' : 'value'}.`;
				return;
			}
		}
		error = '';
		onapply(next);
		open = false;
	}

	function parseValue(row: Row): unknown {
		if (row.kind === 'boolean') return row.boolean;
		if (row.kind === 'number') {
			const value = Number(row.value);
			if (!Number.isFinite(value)) throw new Error('invalid number');
			return value;
		}
		if (row.kind === 'json') return JSON.parse(row.value || 'null');
		return row.value;
	}
</script>

<Popover.Root bind:open>
	<Popover.Trigger class={buttonVariants({ variant: 'outline', size: 'sm' })} {disabled}>
		Properties
	</Popover.Trigger>
	<Popover.Content
		bind:ref={panel}
		align="end"
		class="max-h-[80dvh] w-[min(34rem,calc(100vw-2rem))] overflow-y-auto"
	>
		<Popover.Header>
			<Popover.Title>Properties</Popover.Title>
			<Popover.Description>
				Stored as YAML frontmatter. Title and tags are edited above.
			</Popover.Description>
		</Popover.Header>

		<div class="space-y-2">
			{#each rows as row, index (index)}
				<div class="flex items-start gap-1.5">
					<Input
						class="w-24 shrink-0 font-mono text-[11px]"
						aria-label="Property key"
						bind:value={row.key}
						placeholder="key"
						spellcheck="false"
					/>
					<NativeSelect
						class="w-24 shrink-0"
						size="sm"
						aria-label="Property type"
						bind:value={row.kind}
					>
						<option value="string">Text</option>
						<option value="number">Number</option>
						<option value="boolean">Boolean</option>
						<option value="json">JSON</option>
					</NativeSelect>

					{#if row.kind === 'boolean'}
						<label class="flex min-w-0 flex-1 items-center gap-1.5 py-1.5 text-xs text-neutral-300">
							<Checkbox bind:checked={row.boolean} aria-label="Property value" />
							<span>{row.boolean ? 'true' : 'false'}</span>
						</label>
					{:else if row.kind === 'json'}
						<Textarea
							class="min-h-16 min-w-0 flex-1 py-1.5 font-mono text-[11px]"
							aria-label="Property JSON value"
							bind:value={row.value}
							spellcheck="false"
						></Textarea>
					{:else}
						<Input
							class="min-w-0 flex-1 py-1.5 text-xs"
							aria-label="Property value"
							type={row.kind === 'number' ? 'number' : 'text'}
							bind:value={row.value}
						/>
					{/if}

					<Button
						variant="ghost"
						size="icon-sm"
						type="button"
						aria-label="Remove property"
						onclick={() => removeRow(index)}
					>
						<XIcon />
					</Button>
				</div>
			{/each}
		</div>

		<div class="flex items-center justify-between gap-2">
			<Button variant="outline" size="sm" onclick={addRow}>
				<PlusIcon />
				Property
			</Button>
			<Button size="sm" onclick={apply}>Apply</Button>
		</div>

		{#if error}
			<p class="text-destructive text-xs" role="alert">{error}</p>
		{/if}
		<p class="text-muted-foreground truncate font-mono text-[10px]" title={path}>Location: {path}</p>
	</Popover.Content>
</Popover.Root>
