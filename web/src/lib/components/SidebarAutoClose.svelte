<script lang="ts">
	import { afterNavigate } from '$app/navigation';

	import { useSidebar } from '$lib/components/ui/sidebar/index.js';

	const sidebar = useSidebar();

	afterNavigate(() => {
		sidebar.setOpenMobile(false);
	});
</script>
