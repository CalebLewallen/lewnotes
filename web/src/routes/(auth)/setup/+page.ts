import { redirect } from '@sveltejs/kit';

import { api } from '#lib/api';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	const status = await api.authStatus(fetch);
	if (!status.setup_required) redirect(307, '/login');
	return {};
};
