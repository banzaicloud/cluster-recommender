// Copyright © 2018 Banzai Cloud
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package api

// GetRecommendationParams is a placeholder for the recommendation route's path parameters
// swagger:parameters recommendClusterSetup
type GetRecommendationParams struct {
	// in:path
	Provider string `json:"provider"`
	// in:path
	Region string `json:"region"`
}
