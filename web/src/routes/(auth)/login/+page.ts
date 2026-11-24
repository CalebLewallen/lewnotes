import { redirect } from '@sveltejs/kit';

import { api, ApiError } from '#lib/api';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	const status = await api.authStatus(fetch);
	if (status.setup_required) redirect(307, '/setup');
	try {
		await api.me(fetch);
		redirect(307, '/notes');
	} catch (error) {
		if (error instanceof ApiError && error.status === 401) return {};
		throw error;
	}
};
