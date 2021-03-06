/**
 * Pydio Cells Rest API
 * No description provided (generated by Swagger Codegen https://github.com/swagger-api/swagger-codegen)
 *
 * OpenAPI spec version: 1.0
 * 
 *
 * NOTE: This class is auto generated by the swagger code generator program.
 * https://github.com/swagger-api/swagger-codegen.git
 * Do not edit the class manually.
 *
 */


import ApiClient from '../ApiClient';
import RestSettingsAccessRestPolicy from './RestSettingsAccessRestPolicy';





/**
* The RestSettingsAccess model module.
* @module model/RestSettingsAccess
* @version 1.0
*/
export default class RestSettingsAccess {
    /**
    * Constructs a new <code>RestSettingsAccess</code>.
    * @alias module:model/RestSettingsAccess
    * @class
    */

    constructor() {
        

        
        

        

        
    }

    /**
    * Constructs a <code>RestSettingsAccess</code> from a plain JavaScript object, optionally creating a new instance.
    * Copies all relevant properties from <code>data</code> to <code>obj</code> if supplied or a new instance if not.
    * @param {Object} data The plain JavaScript object bearing properties of interest.
    * @param {module:model/RestSettingsAccess} obj Optional instance to populate.
    * @return {module:model/RestSettingsAccess} The populated <code>RestSettingsAccess</code> instance.
    */
    static constructFromObject(data, obj) {
        if (data) {
            obj = obj || new RestSettingsAccess();

            
            
            

            if (data.hasOwnProperty('Label')) {
                obj['Label'] = ApiClient.convertToType(data['Label'], 'String');
            }
            if (data.hasOwnProperty('Description')) {
                obj['Description'] = ApiClient.convertToType(data['Description'], 'String');
            }
            if (data.hasOwnProperty('Policies')) {
                obj['Policies'] = ApiClient.convertToType(data['Policies'], [RestSettingsAccessRestPolicy]);
            }
        }
        return obj;
    }

    /**
    * @member {String} Label
    */
    Label = undefined;
    /**
    * @member {String} Description
    */
    Description = undefined;
    /**
    * @member {Array.<module:model/RestSettingsAccessRestPolicy>} Policies
    */
    Policies = undefined;








}


